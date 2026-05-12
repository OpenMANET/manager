package frontend

import (
	"errors"
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

// terminalTestServer wires only the terminal handler — no auth — for
// behavior tests. Auth gating is exercised separately via the full
// frontend mux test in server_test.go.
func terminalTestServer(t *testing.T) (*httptest.Server, *terminal.Manager) {
	t.Helper()

	cfg := terminal.DefaultConfig()
	cfg.Shell = "/bin/sh"
	mgr := terminal.New(zerolog.Nop(), cfg)
	s := &Server{log: zerolog.Nop(), term: mgr}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/terminal/ws", s.handleTerminalWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, mgr
}

func TestHandleTerminalWS_HappyPath(t *testing.T) {
	srv, mgr := terminalTestServer(t)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"

	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.Eventually(t, mgr.InUse, time.Second, 10*time.Millisecond)
}

func TestHandleTerminalWS_SecondSessionRejected(t *testing.T) {
	srv, mgr := terminalTestServer(t)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"

	first, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	require.Eventually(t, mgr.InUse, time.Second, 10*time.Millisecond)

	second, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, second.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err = second.ReadMessage()

	var ce *websocket.CloseError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, websocket.ClosePolicyViolation, ce.Code)
}

func TestHandleTerminalWS_DisabledReturns404(t *testing.T) {
	s := &Server{log: zerolog.Nop()}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/terminal/ws", s.handleTerminalWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/terminal/ws")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
