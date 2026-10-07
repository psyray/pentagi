package shell

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"pentagi/pkg/docker"

	"github.com/moby/moby/client"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestNormalizeShell(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":        "zsh",
		"  ":      "zsh",
		"zsh":     "zsh",
		"BASH":    "bash",
		" sh ":    "sh",
		"fish":    "zsh",
		"unknown": "zsh",
		"Bash":    "bash",
		"bogus\n": "zsh",
	}

	for input, want := range cases {
		require.Equal(t, want, NormalizeShell(input), "input=%q", input)
	}
}

func TestShellScriptFallsBack(t *testing.T) {
	t.Parallel()

	require.Contains(t, shellScript("zsh"), "command -v zsh")
	require.Contains(t, shellScript("zsh"), "command -v bash")
	require.Contains(t, shellScript("zsh"), "exec sh -l")
	require.Contains(t, shellScript("bash"), "command -v bash")
	require.Equal(t, "exec sh -l", shellScript("sh"))
}

// TestScrollbackKeepsTheTail checks the bounded buffer keeps recent output and
// stays within its window under sustained writes.
func TestScrollbackKeepsTheTail(t *testing.T) {
	t.Parallel()

	b := newScrollback(8)

	b.Write([]byte("abcd"))
	require.Equal(t, "abcd", string(b.Bytes()))

	b.Write([]byte("efgh"))
	require.Equal(t, "abcdefgh", string(b.Bytes()))

	b.Write([]byte("ij"))
	require.Equal(t, "cdefghij", string(b.Bytes()))

	b.Write([]byte("0123456789AB")) // larger than the window: keep the last 8
	require.Equal(t, "456789AB", string(b.Bytes()))
}

// fakeDocker implements just the exec calls the manager uses. The embedded nil
// interface satisfies the rest of the DockerClient surface.
type fakeDocker struct {
	docker.DockerClient

	mu        sync.Mutex
	n         int
	creates   []client.ExecCreateOptions
	resizes   [][2]uint
	keep      map[string]bool // execID -> is a shell (not a kill helper)
	shellConn net.Conn        // server side of the first shell's PTY
	others    []net.Conn      // server sides kept open so their sessions stay alive
}

func (f *fakeDocker) ContainerExecCreate(
	_ context.Context, _ string, opts client.ExecCreateOptions,
) (client.ExecCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates = append(f.creates, opts)
	f.n++
	if f.keep == nil {
		f.keep = map[string]bool{}
	}
	id := "exec-" + strconv.Itoa(f.n)
	f.keep[id] = !strings.Contains(strings.Join(opts.Cmd, " "), "kill -9")

	return client.ExecCreateResult{ID: id}, nil
}

func (f *fakeDocker) ContainerExecAttach(
	_ context.Context, id string, _ client.ExecAttachOptions,
) (client.HijackedResponse, error) {
	clientConn, serverConn := net.Pipe()

	f.mu.Lock()
	if f.keep[id] {
		if f.shellConn == nil {
			f.shellConn = serverConn
		} else {
			f.others = append(f.others, serverConn)
		}
	} else {
		// Kill/lifecycle exec: nothing to stream.
		_ = serverConn.Close()
	}
	f.mu.Unlock()

	return client.HijackedResponse{Conn: clientConn, Reader: bufio.NewReader(clientConn)}, nil
}

func (f *fakeDocker) ContainerExecInspect(_ context.Context, _ string) (client.ExecInspectResult, error) {
	return client.ExecInspectResult{PID: 4242}, nil
}

func (f *fakeDocker) ContainerExecResize(_ context.Context, _ string, height, width uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizes = append(f.resizes, [2]uint{height, width})

	return nil
}

func (f *fakeDocker) serverConn() net.Conn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shellConn
}

func newTestManager(f *fakeDocker) *Manager {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	return NewManager(f, 2, logger)
}

func TestManagerCreateListClose(t *testing.T) {
	t.Parallel()

	f := &fakeDocker{}
	m := newTestManager(f)

	sess, err := m.Create(context.Background(), 7, "c", "zsh", 80, 24)
	require.NoError(t, err)
	require.Equal(t, "terminal 1", sess.Name())

	list := m.List(7)
	require.Len(t, list, 1)
	require.Equal(t, sess.ID(), list[0].ID)
	require.True(t, list[0].Running)

	// A second terminal gets the next name.
	sess2, err := m.Create(context.Background(), 7, "c", "", 0, 0)
	require.NoError(t, err)
	require.Equal(t, "terminal 2", sess2.Name())

	// The third exceeds the cap of two.
	_, err = m.Create(context.Background(), 7, "c", "bash", 80, 24)
	require.ErrorIs(t, err, ErrTooManyTerminals)

	require.NoError(t, m.Close(7, sess.ID()))
	require.ErrorIs(t, m.Close(7, "missing"), ErrSessionNotFound)

	require.Eventually(t, func() bool {
		return len(m.List(7)) == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestManagerStreamsOutputAndInput(t *testing.T) {
	t.Parallel()

	f := &fakeDocker{}
	m := newTestManager(f)

	sess, err := m.Create(context.Background(), 1, "c", "zsh", 80, 24)
	require.NoError(t, err)

	snapshot, sub, _, err := sess.Subscribe()
	require.NoError(t, err)
	require.Empty(t, snapshot)
	require.NotNil(t, sub)

	// Output from the PTY reaches the subscriber.
	server := f.serverConn()
	require.NotNil(t, server)
	_, err = server.Write([]byte("hello\r\n"))
	require.NoError(t, err)

	select {
	case chunk := <-sub.C():
		require.Equal(t, "hello\r\n", string(chunk))
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PTY output")
	}

	// A late subscriber replays the scrollback.
	replay, sub2, _, err := sess.Subscribe()
	require.NoError(t, err)
	require.Equal(t, "hello\r\n", string(replay))
	sess.Detach(sub2)

	// Client input reaches the PTY. net.Pipe is unbuffered, so the write must
	// run alongside the read rather than before it.
	go func() {
		_ = sess.Write([]byte("id\n"))
	}()

	buf := make([]byte, 3)
	require.NoError(t, server.SetReadDeadline(time.Now().Add(2*time.Second)))
	n, err := server.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "id\n", string(buf[:n]))

	// Resize is forwarded (rows then cols).
	require.NoError(t, sess.Resize(context.Background(), 120, 40))
	f.mu.Lock()
	require.Len(t, f.resizes, 1)
	require.Equal(t, [2]uint{40, 120}, f.resizes[0])
	f.mu.Unlock()

	sess.Detach(sub)
	sess.Close()
}

func TestManagerTagsTheShellAndSweepsOnClose(t *testing.T) {
	t.Parallel()

	f := &fakeDocker{}
	m := newTestManager(f)

	sess, err := m.Create(context.Background(), 3, "c", "zsh", 80, 24)
	require.NoError(t, err)

	// The shell exec carries the session marker so teardown can find its tree.
	f.mu.Lock()
	require.Contains(t, f.creates[0].Env, ShellIDEnvVar+"="+sess.ID())
	f.mu.Unlock()

	sess.Close()

	// Closing runs a sweep exec whose command targets that same marker.
	require.Eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()

		for _, c := range f.creates {
			if len(c.Cmd) == 3 && strings.Contains(c.Cmd[2], ShellIDEnvVar+"="+sess.ID()) {
				return true
			}
		}

		return false
	}, 2*time.Second, 10*time.Millisecond)
}

func TestManagerCreateWithoutDocker(t *testing.T) {
	t.Parallel()

	m := NewManager(nil, 0, nil)
	require.Equal(t, DefaultMaxTerminals, m.maxPerFlow)

	_, err := m.Create(context.Background(), 1, "c", "zsh", 80, 24)
	require.ErrorIs(t, err, ErrDockerUnavailable)
}

func TestSessionSubscribeAfterCloseReportsExit(t *testing.T) {
	t.Parallel()

	f := &fakeDocker{}
	m := newTestManager(f)

	sess, err := m.Create(context.Background(), 1, "c", "zsh", 80, 24)
	require.NoError(t, err)

	// Closing the client side ends the read loop, which finishes the session.
	sess.Close()

	require.Eventually(t, func() bool { return sess.IsDone() }, 2*time.Second, 10*time.Millisecond)

	replay, sub, exitCode, err := sess.Subscribe()
	require.ErrorIs(t, err, ErrSessionClosed)
	require.Nil(t, sub)
	require.NotNil(t, exitCode)
	require.True(t, strings.TrimSpace(string(replay)) != "" || replay == nil)
}
