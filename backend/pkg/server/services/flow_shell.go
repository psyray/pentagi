package services

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"pentagi/pkg/docker"
	"pentagi/pkg/server/logger"
	"pentagi/pkg/server/models"
	"pentagi/pkg/server/response"
	"pentagi/pkg/shell"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jinzhu/gorm"
)

const (
	// shellPongWait is how long the server waits for a client pong before it
	// considers the websocket dead and releases the subscription.
	shellPongWait = 60 * time.Second
	// shellPingInterval must stay well under shellPongWait.
	shellPingInterval = 25 * time.Second
	// shellWriteWait bounds a single websocket write.
	shellWriteWait = 10 * time.Second
	// shellReadLimit caps a client control frame.
	shellReadLimit = 1 << 20
)

var (
	errShellDisabled = response.NewHttpError(
		http.StatusNotFound, "FlowShell.Disabled", "flow shell is not enabled")
	errShellTooMany = response.NewHttpError(
		http.StatusConflict, "FlowShell.TooManyTerminals", "too many terminals for this flow")
	errShellNotFound = response.NewHttpError(
		http.StatusNotFound, "FlowShell.NotFound", "shell terminal not found")
)

// shellCreateRequest is the POST body for a new terminal.
type shellCreateRequest struct {
	Shell string `json:"shell"`
	Cols  uint   `json:"cols"`
	Rows  uint   `json:"rows"`
}

// shellClientMessage is a client control frame (text messages only).
type shellClientMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// FlowShellService exposes interactive shells bound to a flow's primary
// container. Output is a raw PTY stream (binary websocket frames); the client
// sends input as binary frames and control messages (resize/close) as JSON text
// frames.
type FlowShellService struct {
	db           *gorm.DB
	tenantPrefix string
	dockerClient docker.DockerClient
	manager      *shell.Manager
	enabled      bool
	upgrader     websocket.Upgrader
}

func NewFlowShellService(
	db *gorm.DB,
	tenantPrefix string,
	dockerClient docker.DockerClient,
	manager *shell.Manager,
	origins []string,
	enabled bool,
) *FlowShellService {
	ov := newOriginValidator(origins)

	return &FlowShellService{
		db:           db,
		tenantPrefix: tenantPrefix,
		dockerClient: dockerClient,
		manager:      manager,
		enabled:      enabled,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return ov.validateOrigin(r.Header.Get("Origin"), r.Host)
			},
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
		},
	}
}

// GetFlowShells lists the live terminals of a flow.
// @Summary Retrieve the flow's interactive terminals
// @Tags FlowShell
// @Produce json
// @Security BearerAuth
// @Param flowID path int true "flow id" minimum(0)
// @Success 200 {object} response.successResp "terminals list received successful"
// @Failure 403 {object} response.errorResp "not permitted"
// @Failure 404 {object} response.errorResp "flow not found or disabled"
// @Failure 500 {object} response.errorResp "internal error"
// @Router /flows/{flowID}/shell [get]
func (s *FlowShellService) GetFlowShells(c *gin.Context) {
	flowID, err := parseFlowIDParam(c)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error parsing flow id")
		response.Error(c, response.ErrFlowsInvalidRequest, err)
		return
	}

	if !s.enabled {
		response.Error(c, errShellDisabled, nil)
		return
	}

	if _, err := s.getFlow(c, flowID, false); err != nil {
		s.handleFlowLookupError(c, flowID, err)
		return
	}

	shells := s.manager.List(int64(flowID))

	response.Success(c, http.StatusOK, gin.H{"shells": shells, "total": len(shells)})
}

// CreateFlowShell starts a new interactive terminal in the flow's container.
// @Summary Create an interactive terminal
// @Tags FlowShell
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param flowID path int true "flow id" minimum(0)
// @Param request body shellCreateRequest true "shell choice and initial size"
// @Success 200 {object} response.successResp "terminal created"
// @Failure 400 {object} response.errorResp "invalid request"
// @Failure 403 {object} response.errorResp "not permitted"
// @Failure 404 {object} response.errorResp "flow or container not found"
// @Failure 409 {object} response.errorResp "too many terminals for this flow"
// @Failure 500 {object} response.errorResp "internal error"
// @Router /flows/{flowID}/shell [post]
func (s *FlowShellService) CreateFlowShell(c *gin.Context) {
	flowID, err := parseFlowIDParam(c)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error parsing flow id")
		response.Error(c, response.ErrFlowsInvalidRequest, err)
		return
	}

	if !s.enabled {
		response.Error(c, errShellDisabled, nil)
		return
	}

	if _, err := s.getFlow(c, flowID, true); err != nil {
		s.handleFlowLookupError(c, flowID, err)
		return
	}

	var req shellCreateRequest
	// Body is optional: an empty request yields the default shell and size.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.FromContext(c).WithError(err).Error("error binding shell create request")
			response.Error(c, response.ErrFlowsInvalidRequest, err)
			return
		}
	}

	containerName, ok := s.runningContainer(c, flowID)
	if !ok {
		return
	}

	sess, err := s.manager.Create(c.Request.Context(), int64(flowID), containerName, req.Shell, req.Cols, req.Rows)
	if err != nil {
		s.handleCreateError(c, flowID, err)
		return
	}

	response.Success(c, http.StatusOK, sess.Info())
}

// DeleteFlowShell terminates one terminal.
// @Summary Close an interactive terminal
// @Tags FlowShell
// @Produce json
// @Security BearerAuth
// @Param flowID path int true "flow id" minimum(0)
// @Param terminalID path string true "terminal id"
// @Success 200 {object} response.successResp "terminal closed"
// @Failure 403 {object} response.errorResp "not permitted"
// @Failure 404 {object} response.errorResp "flow or terminal not found"
// @Failure 500 {object} response.errorResp "internal error"
// @Router /flows/{flowID}/shell/{terminalID} [delete]
func (s *FlowShellService) DeleteFlowShell(c *gin.Context) {
	flowID, err := parseFlowIDParam(c)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error parsing flow id")
		response.Error(c, response.ErrFlowsInvalidRequest, err)
		return
	}

	if !s.enabled {
		response.Error(c, errShellDisabled, nil)
		return
	}

	if _, err := s.getFlow(c, flowID, true); err != nil {
		s.handleFlowLookupError(c, flowID, err)
		return
	}

	if err := s.manager.Close(int64(flowID), c.Param("terminalID")); err != nil {
		logger.FromContext(c).WithError(err).WithField("flow_id", flowID).Error("error closing shell terminal")
		response.Error(c, errShellNotFound, err)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"closed": true})
}

// AttachFlowShell upgrades to a websocket and bridges it to the terminal.
// @Summary Attach to an interactive terminal
// @Tags FlowShell
// @Security BearerAuth
// @Param flowID path int true "flow id" minimum(0)
// @Param terminalID path string true "terminal id"
// @Router /flows/{flowID}/shell/{terminalID}/ws [get]
func (s *FlowShellService) AttachFlowShell(c *gin.Context) {
	flowID, err := parseFlowIDParam(c)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error parsing flow id")
		response.Error(c, response.ErrFlowsInvalidRequest, err)
		return
	}

	if !s.enabled {
		response.Error(c, errShellDisabled, nil)
		return
	}

	// Validate auth, ownership and terminal existence before the upgrade so the
	// client gets a proper JSON error instead of a bare socket failure.
	if _, err := s.getFlow(c, flowID, true); err != nil {
		s.handleFlowLookupError(c, flowID, err)
		return
	}

	sess, ok := s.manager.Get(int64(flowID), c.Param("terminalID"))
	if !ok {
		response.Error(c, errShellNotFound, nil)
		return
	}

	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error upgrading shell websocket")
		return
	}

	defer conn.Close()

	s.pump(conn, sess)
}

// pump bridges the websocket and the session. Only this goroutine writes to the
// websocket, so the gorilla connection is never written concurrently.
func (s *FlowShellService) pump(ws *websocket.Conn, sess *shell.Session) {
	snapshot, sub, exitCode, err := sess.Subscribe()

	if len(snapshot) > 0 {
		if werr := writeBinary(ws, snapshot); werr != nil {
			sess.Detach(sub)
			return
		}
	}

	if err != nil {
		// Session already finished: replay the buffer then report the exit.
		writeExit(ws, exitCode)
		return
	}

	defer sess.Detach(sub)

	ws.SetReadLimit(shellReadLimit)
	_ = ws.SetReadDeadline(time.Now().Add(shellPongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(shellPongWait))
	})

	readErr := make(chan error, 1)
	go func() {
		readErr <- s.readClient(ws, sess)
	}()

	ping := time.NewTicker(shellPingInterval)
	defer ping.Stop()

	for {
		select {
		case chunk, ok := <-sub.C():
			if !ok {
				code, _ := sess.ExitCode()
				writeExit(ws, &code)

				return
			}

			if werr := writeBinary(ws, chunk); werr != nil {
				return
			}
		case <-readErr:
			return
		case <-sess.Done():
			code, _ := sess.ExitCode()
			writeExit(ws, &code)

			return
		case <-ping.C:
			if werr := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(shellWriteWait)); werr != nil {
				return
			}
		}
	}
}

// readClient forwards client frames to the shell: binary frames are raw input,
// text frames are JSON control messages.
func (s *FlowShellService) readClient(ws *websocket.Conn, sess *shell.Session) error {
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}

		switch mt {
		case websocket.BinaryMessage:
			if len(data) > 0 {
				if err := sess.Write(data); err != nil {
					return err
				}
			}
		case websocket.TextMessage:
			var msg shellClientMessage
			if json.Unmarshal(data, &msg) != nil {
				continue
			}

			switch msg.Type {
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					_ = sess.Resize(context.Background(), uint(msg.Cols), uint(msg.Rows))
				}
			case "close":
				return nil
			}
		}
	}
}

// runningContainer resolves the flow's primary container, verifying docker is
// configured and the container is running. It writes the error response itself
// and returns ok=false when the caller must stop.
func (s *FlowShellService) runningContainer(c *gin.Context, flowID uint64) (string, bool) {
	if s.dockerClient == nil || s.manager == nil {
		response.Error(c, response.ErrInternal, nil)
		return "", false
	}

	containerName := primaryContainerName(s.tenantPrefix, flowID)

	running, err := s.dockerClient.IsContainerRunning(c.Request.Context(), containerName)
	if err != nil {
		logger.FromContext(c).WithError(err).WithField("flow_id", flowID).Error("error checking shell container")
		response.Error(c, response.ErrInternal, err)
		return "", false
	}

	if !running {
		logger.FromContext(c).WithField("flow_id", flowID).Warn("shell requested for a flow whose container is not running")
		response.Error(c, response.ErrContainersNotFound, nil)
		return "", false
	}

	return containerName, true
}

func (s *FlowShellService) handleCreateError(c *gin.Context, flowID uint64, err error) {
	fields := map[string]any{"flow_id": flowID}

	switch err {
	case shell.ErrDockerUnavailable:
		logger.FromContext(c).WithFields(fields).Error("docker client unavailable for shell")
		response.Error(c, response.ErrInternal, err)
	case shell.ErrTooManyTerminals:
		logger.FromContext(c).WithFields(fields).Warn("too many shell terminals for flow")
		response.Error(c, errShellTooMany, nil)
	default:
		logger.FromContext(c).WithError(err).WithFields(fields).Error("error creating shell terminal")
		response.Error(c, response.ErrInternal, err)
	}
}

// getFlow resolves the flow with the same owner/admin scope as flow files.
func (s *FlowShellService) getFlow(c *gin.Context, flowID uint64, writeAccess bool) (models.Flow, error) {
	var flow models.Flow

	uid := c.GetUint64("uid")
	privs := c.GetStringSlice("prm")
	scope := flowScopeForFiles(privs, uid, flowID, writeAccess)
	if scope == nil {
		return flow, response.ErrNotPermitted
	}

	if err := s.db.Model(&flow).Scopes(scope).Take(&flow).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return flow, response.ErrFlowsNotFound
		}
		return flow, err
	}

	return flow, nil
}

func (s *FlowShellService) handleFlowLookupError(c *gin.Context, flowID uint64, err error) {
	fields := map[string]any{"flow_id": flowID}

	switch err {
	case response.ErrNotPermitted:
		logger.FromContext(c).WithFields(fields).Error("error filtering user role permissions: permission not found")
		response.Error(c, response.ErrNotPermitted, nil)
	case response.ErrFlowsNotFound:
		logger.FromContext(c).WithFields(fields).Error("error finding flow for shell")
		response.Error(c, response.ErrFlowsNotFound, err)
	default:
		logger.FromContext(c).WithError(err).WithFields(fields).Error("error finding flow for shell")
		response.Error(c, response.ErrInternal, err)
	}
}

func writeBinary(ws *websocket.Conn, data []byte) error {
	if err := ws.SetWriteDeadline(time.Now().Add(shellWriteWait)); err != nil {
		return err
	}

	return ws.WriteMessage(websocket.BinaryMessage, data)
}

func writeExit(ws *websocket.Conn, code *int) {
	payload := map[string]any{"type": "exit"}
	if code != nil {
		payload["code"] = *code
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	_ = ws.SetWriteDeadline(time.Now().Add(shellWriteWait))
	_ = ws.WriteMessage(websocket.TextMessage, data)
}
