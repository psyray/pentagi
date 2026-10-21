package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pentagi/pkg/docker"
	"pentagi/pkg/shell"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/moby/moby/client"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// wsTestDocker is a controllable docker double for the shell websocket bridge:
// it records client input, exposes the PTY server side so the test can push
// output, and keeps shell execs alive (kill helpers are closed immediately).
type wsTestDocker struct {
	docker.DockerClient

	mu       sync.Mutex
	n        int
	running  bool
	keep     map[string]bool
	conns    map[string]net.Conn
	shellIDs []string
	inputs   []string
	resizes  [][2]uint
}

func newWSTestDocker() *wsTestDocker {
	return &wsTestDocker{running: true, keep: map[string]bool{}, conns: map[string]net.Conn{}}
}

func (d *wsTestDocker) IsContainerRunning(context.Context, string) (bool, error) {
	return d.running, nil
}

func (d *wsTestDocker) ContainerExecCreate(
	_ context.Context, _ string, opts client.ExecCreateOptions,
) (client.ExecCreateResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	id := fmt.Sprintf("exec-%d", d.n)
	isShell := !strings.Contains(strings.Join(opts.Cmd, " "), "kill -9")
	d.keep[id] = isShell
	if isShell {
		d.shellIDs = append(d.shellIDs, id)
	}

	return client.ExecCreateResult{ID: id}, nil
}

func (d *wsTestDocker) ContainerExecAttach(
	_ context.Context, id string, _ client.ExecAttachOptions,
) (client.HijackedResponse, error) {
	clientConn, serverConn := net.Pipe()

	d.mu.Lock()
	if d.keep[id] {
		d.conns[id] = serverConn
		go d.drainInput(id, serverConn)
	} else {
		_ = serverConn.Close()
	}
	d.mu.Unlock()

	return client.HijackedResponse{Conn: clientConn, Reader: bufio.NewReader(clientConn)}, nil
}

func (d *wsTestDocker) drainInput(id string, conn net.Conn) {
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			d.mu.Lock()
			d.inputs = append(d.inputs, string(buf[:n]))
			d.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (d *wsTestDocker) ContainerExecInspect(context.Context, string) (client.ExecInspectResult, error) {
	return client.ExecInspectResult{PID: 999}, nil
}

func (d *wsTestDocker) ContainerExecResize(_ context.Context, _ string, height, width uint) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resizes = append(d.resizes, [2]uint{height, width})

	return nil
}

func (d *wsTestDocker) writeOutput(t *testing.T, p []byte) {
	t.Helper()
	d.mu.Lock()
	require.NotEmpty(t, d.shellIDs, "no shell exec was created")
	conn := d.conns[d.shellIDs[0]]
	d.mu.Unlock()
	require.NotNil(t, conn)
	_, err := conn.Write(p)
	require.NoError(t, err)
}

func (d *wsTestDocker) sawInput(want string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, in := range d.inputs {
		if strings.Contains(in, want) {
			return true
		}
	}

	return false
}

func newShellTestServer(t *testing.T, svc *FlowShellService) *httptest.Server {
	t.Helper()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("uid", uint64(42))
		c.Set("prm", []string{"flow_files.view", "flow_files.upload"})
		c.Next()
	})
	engine.GET("/flows/:flowID/shell/", svc.GetFlowShells)
	engine.POST("/flows/:flowID/shell/", svc.CreateFlowShell)
	engine.DELETE("/flows/:flowID/shell/:terminalID", svc.DeleteFlowShell)
	engine.GET("/flows/:flowID/shell/:terminalID/ws", svc.AttachFlowShell)

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	return srv
}

func TestFlowShell_DisabledReturnsNotFound(t *testing.T) {
	db := setupFlowFileServiceTestDB(t)
	seedFlow(t, db, 1, 42)

	svc := NewFlowShellService(db, "", newWSTestDocker(), nil, []string{"*"}, false)
	srv := newShellTestServer(t, svc)

	resp, err := http.Post(srv.URL+"/flows/1/shell/", "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFlowShell_PermissionDeniedWithoutPrivilege(t *testing.T) {
	db := setupFlowFileServiceTestDB(t)
	seedFlow(t, db, 1, 42)

	fake := newWSTestDocker()
	mgr := shell.NewManager(fake, 5, logrus.New())
	svc := NewFlowShellService(db, "", fake, mgr, []string{"*"}, true)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("uid", uint64(42))
		c.Set("prm", []string{}) // no privileges
		c.Next()
	})
	engine.POST("/flows/:flowID/shell/", svc.CreateFlowShell)

	srv := httptest.NewServer(engine)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/flows/1/shell/", "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestFlowShell_CreateAttachStreamAndClose(t *testing.T) {
	db := setupFlowFileServiceTestDB(t)
	seedFlow(t, db, 1, 42)

	fake := newWSTestDocker()
	mgr := shell.NewManager(fake, 5, logrus.New())
	svc := NewFlowShellService(db, "", fake, mgr, []string{"*"}, true)
	srv := newShellTestServer(t, svc)

	// Create a terminal.
	body := strings.NewReader(`{"shell":"bash","cols":100,"rows":30}`)
	resp, err := http.Post(srv.URL+"/flows/1/shell/", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var created struct {
		Data shell.Info `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	require.NotEmpty(t, created.Data.ID)
	require.Equal(t, "terminal 1", created.Data.Name)
	require.Equal(t, "bash", created.Data.Shell)

	// Attach over websocket.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/flows/1/shell/" + created.Data.ID + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Client input reaches the PTY.
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("whoami\n")))
	require.Eventually(t, func() bool { return fake.sawInput("whoami") }, 2*time.Second, 10*time.Millisecond)

	// PTY output reaches the client.
	fake.writeOutput(t, []byte("root\n"))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	mt, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.BinaryMessage, mt)
	require.Equal(t, "root\n", string(msg))

	// Resize control frame is forwarded.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":120,"rows":40}`)))
	require.Eventually(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return len(fake.resizes) == 1
	}, 2*time.Second, 10*time.Millisecond)
	fake.mu.Lock()
	require.Equal(t, [2]uint{40, 120}, fake.resizes[0])
	fake.mu.Unlock()

	// Closing the terminal reports the exit to the attached client.
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/flows/1/shell/"+created.Data.ID, nil)
	require.NoError(t, err)
	delResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer delResp.Body.Close()
	require.Equal(t, http.StatusOK, delResp.StatusCode)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	mt, msg, err = conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, mt)
	require.Contains(t, string(msg), `"type":"exit"`)
}
