package shell

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"pentagi/pkg/docker"

	"github.com/moby/moby/client"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultMaxTerminals is the per-flow cap when none is configured.
	DefaultMaxTerminals = 5
	// defaultCols/defaultRows seed the PTY before the client sends a real size.
	defaultCols = 80
	defaultRows = 24
)

// ErrDockerUnavailable is returned when the manager has no docker client.
var ErrDockerUnavailable = errors.New("docker client is not configured")

// ErrTooManyTerminals is returned when a flow already holds the maximum number
// of live terminals.
var ErrTooManyTerminals = errors.New("too many terminals for this flow")

// ErrSessionNotFound is returned when no session matches the given id.
var ErrSessionNotFound = errors.New("shell session not found")

// Manager owns the interactive shells, keyed by flow. Sessions live in memory
// and die with their container; there is no persistence.
type Manager struct {
	dc         docker.DockerClient
	maxPerFlow int
	logger     *logrus.Logger

	mu    sync.Mutex
	flows map[int64]*flowRegistry
	seq   uint64
}

type flowRegistry struct {
	sessions map[string]*Session
	counter  int
}

// NewManager builds a manager. A non-positive maxPerFlow falls back to
// DefaultMaxTerminals.
func NewManager(dc docker.DockerClient, maxPerFlow int, logger *logrus.Logger) *Manager {
	if maxPerFlow <= 0 {
		maxPerFlow = DefaultMaxTerminals
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &Manager{
		dc:         dc,
		maxPerFlow: maxPerFlow,
		logger:     logger,
		flows:      map[int64]*flowRegistry{},
	}
}

// Create starts a new interactive shell in the flow's container. It returns
// ErrDockerUnavailable when docker is not configured and ErrTooManyTerminals
// when the flow already holds the maximum.
func (m *Manager) Create(
	ctx context.Context,
	flowID int64,
	container string,
	shellName string,
	cols, rows uint,
) (*Session, error) {
	if m == nil || m.dc == nil {
		return nil, ErrDockerUnavailable
	}

	shellName = NormalizeShell(shellName)
	if cols == 0 {
		cols = defaultCols
	}
	if rows == 0 {
		rows = defaultRows
	}

	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("t%d-%d", flowID, m.seq)
	m.mu.Unlock()

	created, err := m.dc.ContainerExecCreate(ctx, container, client.ExecCreateOptions{
		Cmd:          []string{"/bin/sh", "-c", shellScript(shellName)},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          true,
		ConsoleSize:  client.ConsoleSize{Height: rows, Width: cols},
		WorkingDir:   docker.WorkFolderPathInContainer,
		// Tag every process of this session so terminate() can sweep the tree
		// without depending on docker's (host) exec PID.
		Env: []string{ShellIDEnvVar + "=" + id},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create shell exec: %w", err)
	}

	resp, err := m.dc.ContainerExecAttach(ctx, created.ID, client.ExecAttachOptions{TTY: true})
	if err != nil {
		return nil, fmt.Errorf("failed to attach shell exec: %w", err)
	}

	sess := newSession(m.dc, id, flowID, container, shellName, created.ID, resp.Conn, resp.Reader, m.logger)
	sess.onDone = m.remove

	m.mu.Lock()
	reg := m.flows[flowID]
	if reg == nil {
		reg = &flowRegistry{sessions: map[string]*Session{}}
		m.flows[flowID] = reg
	}
	for existingID, existing := range reg.sessions {
		if existing.IsDone() {
			delete(reg.sessions, existingID)
		}
	}
	if len(reg.sessions) >= m.maxPerFlow {
		m.mu.Unlock()
		sess.Close()

		return nil, ErrTooManyTerminals
	}
	reg.counter++
	sess.name = fmt.Sprintf("terminal %d", reg.counter)
	reg.sessions[id] = sess
	m.mu.Unlock()

	go sess.readLoop()

	return sess, nil
}

// List returns the live sessions for a flow, oldest first.
func (m *Manager) List(flowID int64) []Info {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	reg := m.flows[flowID]
	if reg == nil {
		return nil
	}

	infos := make([]Info, 0, len(reg.sessions))
	for _, s := range reg.sessions {
		if s.IsDone() {
			continue
		}
		infos = append(infos, s.Info())
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].CreatedAt.Before(infos[j].CreatedAt)
	})

	return infos
}

// Get returns a session by id, done or not.
func (m *Manager) Get(flowID int64, id string) (*Session, bool) {
	if m == nil {
		return nil, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	reg := m.flows[flowID]
	if reg == nil {
		return nil, false
	}

	s, ok := reg.sessions[id]

	return s, ok
}

// Close terminates one session. It returns ErrSessionNotFound when unknown.
func (m *Manager) Close(flowID int64, id string) error {
	s, ok := m.Get(flowID, id)
	if !ok {
		return ErrSessionNotFound
	}

	s.Close()

	return nil
}

// CloseFlow terminates every session of a flow. Used when the flow stops or its
// container is removed.
func (m *Manager) CloseFlow(flowID int64) {
	if m == nil {
		return
	}

	m.mu.Lock()
	reg := m.flows[flowID]
	delete(m.flows, flowID)
	m.mu.Unlock()

	if reg == nil {
		return
	}

	for _, s := range reg.sessions {
		s.Close()
	}
}

// remove unregisters a finished session.
func (m *Manager) remove(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reg := m.flows[s.flowID]
	if reg == nil {
		return
	}

	if reg.sessions[s.id] == s {
		delete(reg.sessions, s.id)
	}

	if len(reg.sessions) == 0 {
		delete(m.flows, s.flowID)
	}
}
