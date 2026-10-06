package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"pentagi/pkg/docker"

	"github.com/moby/moby/client"
	"github.com/sirupsen/logrus"
)

const (
	// scrollbackSize bounds how much output a reattaching client receives.
	scrollbackSize = 1 << 20 // 1 MiB
	// subscriberQueue is how many output frames a subscriber may lag behind
	// before it is considered stuck and disconnected.
	subscriberQueue = 256
	// readChunk is the per-read buffer size from the PTY.
	readChunk = 32 * 1024
	// inspectTimeout bounds the ExecInspect call used to learn the exit code.
	inspectTimeout = 5 * time.Second
	// killTimeout bounds the best-effort teardown exec on close.
	killTimeout = 5 * time.Second
)

// ShellIDEnvVar tags every process of a shell session so it can be killed
// without relying on docker's exec PID (which is not the container-namespace
// PID and therefore useless here). Every child inherits it, so the sweep takes
// the whole process tree down.
const ShellIDEnvVar = "PENTAGI_SHELL_ID"

// ErrSessionClosed is returned by Subscribe when the shell has already exited.
var ErrSessionClosed = errors.New("shell session is closed")

// Info is the client-facing view of a session.
type Info struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Shell     string    `json:"shell"`
	CreatedAt time.Time `json:"createdAt"`
	Running   bool      `json:"running"`
}

// Session is a live interactive shell inside a flow's container. Its lifecycle
// is decoupled from any single websocket client: output is buffered and fanned
// out so a client may detach and reattach with replay.
type Session struct {
	id        string
	name      string
	flowID    int64
	container string
	shell     string
	execID    string
	createdAt time.Time

	dc     docker.DockerClient
	conn   net.Conn
	reader *bufio.Reader

	writeMu sync.Mutex

	mu         sync.Mutex
	scrollback *scrollback
	subs       map[*Subscription]struct{}
	done       bool
	exitCode   *int
	finished   chan struct{}
	closeOnce  sync.Once

	onDone func(*Session)
	logger *logrus.Logger
}

func newSession(
	dc docker.DockerClient,
	id string,
	flowID int64,
	container string,
	shellName string,
	execID string,
	conn net.Conn,
	reader *bufio.Reader,
	logger *logrus.Logger,
) *Session {
	return &Session{
		id:         id,
		flowID:     flowID,
		container:  container,
		shell:      shellName,
		execID:     execID,
		createdAt:  time.Now(),
		dc:         dc,
		conn:       conn,
		reader:     reader,
		scrollback: newScrollback(scrollbackSize),
		subs:       map[*Subscription]struct{}{},
		finished:   make(chan struct{}),
		logger:     logger,
	}
}

// ID returns the session identifier.
func (s *Session) ID() string { return s.id }

// Name returns the human-facing name ("terminal N").
func (s *Session) Name() string { return s.name }

// FlowID returns the owning flow.
func (s *Session) FlowID() int64 { return s.flowID }

// IsDone reports whether the shell process has exited or been closed.
func (s *Session) IsDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.done
}

// Info builds the client-facing view of the session.
func (s *Session) Info() Info {
	return Info{
		ID:        s.id,
		Name:      s.name,
		Shell:     s.shell,
		CreatedAt: s.createdAt,
		Running:   !s.IsDone(),
	}
}

// readLoop drains the PTY, fanning output out to the scrollback and the current
// subscribers, and finishes the session on EOF (the shell exited or the
// container stopped).
func (s *Session) readLoop() {
	buf := make([]byte, readChunk)

	for {
		n, err := s.reader.Read(buf)

		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.publish(chunk)
		}

		if err != nil {
			s.finish(err)

			return
		}
	}
}

// publish appends output to the scrollback and fans it out. A subscriber that
// cannot keep up is dropped rather than allowed to stall the PTY reader.
func (s *Session) publish(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done {
		return
	}

	s.scrollback.Write(p)

	for sub := range s.subs {
		select {
		case sub.ch <- p:
		default:
			sub.close()
			delete(s.subs, sub)
		}
	}
}

// Subscribe returns the buffered output so far plus a live subscription. When
// the session is already closed it returns the buffer with ErrSessionClosed and
// a nil subscription, so the caller can replay and then close.
func (s *Session) Subscribe() ([]byte, *Subscription, *int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := append([]byte(nil), s.scrollback.Bytes()...)

	if s.done {
		return snapshot, nil, s.exitCode, ErrSessionClosed
	}

	sub := newSubscription(subscriberQueue)
	s.subs[sub] = struct{}{}

	return snapshot, sub, nil, nil
}

// Detach removes a subscription. Idempotent and safe to call after the session
// has already finished.
func (s *Session) Detach(sub *Subscription) {
	if sub == nil {
		return
	}

	s.mu.Lock()
	delete(s.subs, sub)
	s.mu.Unlock()

	sub.close()
}

// Write forwards client input to the shell's stdin.
func (s *Session) Write(p []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.conn == nil {
		return net.ErrClosed
	}

	_, err := s.conn.Write(p)

	return err
}

// Resize updates the pseudo-TTY size. cols/rows come from the client; the
// docker API expects height then width.
func (s *Session) Resize(ctx context.Context, cols, rows uint) error {
	if cols == 0 || rows == 0 {
		return nil
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.conn == nil {
		return net.ErrClosed
	}

	return s.dc.ContainerExecResize(ctx, s.execID, rows, cols)
}

// Done returns a channel closed when the session finishes.
func (s *Session) Done() <-chan struct{} { return s.finished }

// ExitCode returns the process exit code once known.
func (s *Session) ExitCode() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.exitCode == nil {
		return 0, false
	}

	return *s.exitCode, true
}

// Close terminates the shell process and releases its resources. It is safe to
// call more than once.
func (s *Session) Close() {
	s.terminate()
	s.finish(net.ErrClosed)
}

// finish performs the one-shot teardown: capture the exit code, mark the
// session done, wake every subscriber, close the PTY and unregister.
func (s *Session) finish(cause error) {
	s.closeOnce.Do(func() {
		exit := s.inspectExitCode()

		s.mu.Lock()
		s.done = true
		s.exitCode = exit
		subs := make([]*Subscription, 0, len(s.subs))
		for sub := range s.subs {
			subs = append(subs, sub)
			delete(s.subs, sub)
		}
		s.mu.Unlock()

		for _, sub := range subs {
			sub.close()
		}

		if s.conn != nil {
			_ = s.conn.Close()
		}

		close(s.finished)

		if s.onDone != nil {
			s.onDone(s)
		}

		if s.logger != nil && cause != nil &&
			!errors.Is(cause, io.EOF) && !errors.Is(cause, net.ErrClosed) {
			s.logger.WithError(cause).Debug("shell session finished")
		}
	})
}

// terminate kills the shell's process tree. Closing the hijacked connection
// alone leaves the exec running in the container, so a shell would accumulate on
// every detach. Docker's exec PID is the host PID, not the container-namespace
// PID, so the sweep matches on the session's environment marker instead.
func (s *Session) terminate() {
	ctx, cancel := context.WithTimeout(context.Background(), killTimeout)
	defer cancel()

	created, err := s.dc.ContainerExecCreate(ctx, s.container, client.ExecCreateOptions{
		Cmd: []string{"sh", "-c", killShellScript(s.id)},
	})
	if err != nil {
		return
	}

	resp, err := s.dc.ContainerExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return
	}

	_, _ = io.Copy(io.Discard, resp.Reader)
	resp.Close()
}

// killShellScript scans /proc for processes carrying this session's marker and
// kills them (SIGKILL). Reading another process's environ requires the same
// uid, which the shell's root user satisfies.
func killShellScript(id string) string {
	return fmt.Sprintf(
		`want='%s=%s'
`+
			`for p in /proc/[0-9]*; do
`+
			`  [ -r "$p/environ" ] || continue
`+
			`  if tr '\0' '\n' < "$p/environ" 2>/dev/null | grep -qx "$want"; then
`+
			`    kill -9 "${p#/proc/}" 2>/dev/null
`+
			`  fi
`+
			`done
`+
			`exit 0`,
		ShellIDEnvVar, id,
	)
}

func (s *Session) inspectExitCode() *int {
	ctx, cancel := context.WithTimeout(context.Background(), inspectTimeout)
	defer cancel()

	res, err := s.dc.ContainerExecInspect(ctx, s.execID)
	if err != nil {
		return nil
	}

	code := res.ExitCode

	return &code
}

// Subscription receives output frames until it is closed.
type Subscription struct {
	ch   chan []byte
	once sync.Once
}

func newSubscription(size int) *Subscription {
	return &Subscription{ch: make(chan []byte, size)}
}

// C exposes the output channel; it is closed when the session finishes or the
// subscription is detached.
func (s *Subscription) C() <-chan []byte {
	return s.ch
}

func (s *Subscription) close() {
	s.once.Do(func() { close(s.ch) })
}

// scrollback is a bounded byte buffer keeping the most recent output. It uses a
// lazy offset so appends stay amortized O(1) even under sustained output, and
// compacts only once the dropped prefix grows past the window.
type scrollback struct {
	data []byte
	off  int
	max  int
}

func newScrollback(max int) *scrollback {
	return &scrollback{max: max}
}

func (b *scrollback) Write(p []byte) {
	if len(p) >= b.max {
		b.data = append(b.data[:0], p[len(p)-b.max:]...)
		b.off = 0

		return
	}

	b.data = append(b.data, p...)

	if len(b.data)-b.off > b.max {
		b.off = len(b.data) - b.max
	}

	if b.off > b.max {
		b.data = append(b.data[:0], b.data[b.off:]...)
		b.off = 0
	}
}

func (b *scrollback) Bytes() []byte {
	return b.data[b.off:]
}
