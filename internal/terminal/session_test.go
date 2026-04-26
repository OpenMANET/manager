package terminal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openmanet/openmanetd/internal/terminal"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialTestServer starts an httptest server whose only handler upgrades the
// request and runs a single Session bound to /bin/sh, returning the client
// connection.
func dialTestServer(t *testing.T, cfg terminal.Config) (*websocket.Conn, func()) {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)

			return
		}

		s, err := terminal.NewSession(r.Context(), zerolog.Nop(), cfg, conn)
		if err != nil {
			_ = conn.Close()

			return
		}

		_ = s.Run(r.Context())
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	return c, srv.Close
}

func TestSession_EchoRoundTrip(t *testing.T) {
	cfg := terminal.DefaultConfig()
	cfg.Shell = "/bin/sh" // present in dev container; ash may not be

	conn, _ := dialTestServer(t, cfg)
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("echo hello-tty\n")))

	deadline := time.Now().Add(3 * time.Second)
	require.NoError(t, conn.SetReadDeadline(deadline))

	var combined strings.Builder

	for time.Now().Before(deadline) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		combined.Write(data)

		if strings.Contains(combined.String(), "hello-tty") {
			break
		}
	}

	assert.Contains(t, combined.String(), "hello-tty")
}

func TestSession_ResizeControlMessage(t *testing.T) {
	cfg := terminal.DefaultConfig()
	cfg.Shell = "/bin/sh"

	conn, _ := dialTestServer(t, cfg)

	// Send resize, then `stty size` to read it back.
	resize, err := json.Marshal(map[string]any{"type": "resize", "cols": 132, "rows": 43})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, resize))

	// stty needs a stable PTY first; wait briefly so the resize ioctl is applied.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("stty size\n")))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	var combined strings.Builder

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		combined.Write(data)

		if strings.Contains(combined.String(), "43 132") {
			break
		}
	}

	assert.Contains(t, combined.String(), "43 132")
}

func TestSession_ContextCancelTerminatesShell(t *testing.T) {
	cfg := terminal.DefaultConfig()
	cfg.Shell = "/bin/sh"
	cfg.GraceShutdown = 200 * time.Millisecond

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ctx, cancel := context.WithCancel(context.Background())
	srvDone := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		s, _ := terminal.NewSession(ctx, zerolog.Nop(), cfg, conn)
		_ = s.Run(ctx)

		close(srvDone)
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	// Start a long-running command, then cancel.
	require.NoError(t, c.WriteMessage(websocket.BinaryMessage, []byte("sleep 30\n")))
	// Allow the shell to fork sleep(1) before canceling; without this the
	// cancel could race with sh parsing the command, leaving an empty
	// process group.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-srvDone:
		// Session returned within the grace window — shell was reaped.
	case <-time.After(3 * time.Second):
		t.Fatal("session did not exit after context cancel")
	}
}

func TestSession_ShellExitGraceful(t *testing.T) {
	cfg := terminal.DefaultConfig()
	cfg.Shell = "/bin/sh"

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srvDone := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		s, err := terminal.NewSession(r.Context(), zerolog.Nop(), cfg, conn)
		if err != nil {
			_ = conn.Close()

			return
		}

		_ = s.Run(r.Context())

		close(srvDone)
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	// Type `exit 0` to make the shell terminate on its own.
	require.NoError(t, c.WriteMessage(websocket.BinaryMessage, []byte("exit 0\n")))

	// Drain output until the WS closes (which happens when Run returns and
	// the handler's goroutine exits — gorilla closes the conn on handler return).
	require.NoError(t, c.SetReadDeadline(time.Now().Add(3*time.Second)))

	for {
		if _, _, rerr := c.ReadMessage(); rerr != nil {
			break
		}
	}

	select {
	case <-srvDone:
		// Run returned cleanly after shell exit.
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after shell exited gracefully")
	}
}
