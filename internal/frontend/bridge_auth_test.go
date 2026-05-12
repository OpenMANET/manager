package frontend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBridgeAuthTransport_AttachesBearerToken verifies the transport injects
// an Authorization: Bearer header that the API server's auth middleware
// will accept.
func TestBridgeAuthTransport_AttachesBearerToken(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	var gotAuth atomic.Value

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	transport := newBridgeAuthTransport(ctx, http.DefaultTransport, store, time.Hour)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	resp.Body.Close()

	header, _ := gotAuth.Load().(string)
	assert.NotEmpty(t, header)
	assert.True(t, len(header) > len("Bearer ") && header[:7] == "Bearer ",
		"expected Authorization: Bearer ..., got %q", header)

	// The token must be a real session in the store, so the auth middleware
	// will accept it.
	token := header[len("Bearer "):]
	sess, ok := store.Validate(token)
	require.True(t, ok, "token attached by transport must validate against the store")
	assert.Equal(t, bridgeUsername, sess.Username)
}

// TestBridgeAuthTransport_AuthenticatesAgainstAuthMiddleware proves the
// end-to-end contract: a client wrapped by bridgeAuthTransport gets past
// the same auth middleware that protects every other API endpoint.
func TestBridgeAuthTransport_AuthenticatesAgainstAuthMiddleware(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	mux := http.NewServeMux()
	mux.HandleFunc("/comms/SetTalkGroup", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, bridgeUsername, auth.UsernameFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	})

	authMW := auth.NewAPIAuthMiddleware(store, true)

	upstream := httptest.NewServer(authMW(mux))
	t.Cleanup(upstream.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	transport := newBridgeAuthTransport(ctx, http.DefaultTransport, store, time.Hour)
	client := &http.Client{Transport: transport}

	resp, err := client.Post(upstream.URL+"/comms/SetTalkGroup", "application/json", nil)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"bridge transport must produce a request the API auth middleware accepts")
}

// TestBridgeAuthTransport_RefreshesToken verifies the goroutine mints fresh
// tokens on the configured cadence so a long-running daemon never sees its
// session age out.
func TestBridgeAuthTransport_RefreshesToken(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Refresh aggressively so the test isn't slow.
	transport := newBridgeAuthTransport(ctx, http.DefaultTransport, store, 25*time.Millisecond)

	transport.mu.RLock()
	first := transport.token
	transport.mu.RUnlock()
	require.NotEmpty(t, first)

	require.Eventually(t, func() bool {
		transport.mu.RLock()
		defer transport.mu.RUnlock()

		return transport.token != first
	}, 500*time.Millisecond, 10*time.Millisecond,
		"token should rotate when refresh ticker fires")
}

// TestBridgeAuthTransport_RefreshLoopExitsOnContextCancel ensures the
// background goroutine does not leak when the daemon shuts down.
func TestBridgeAuthTransport_RefreshLoopExitsOnContextCancel(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})

	transport := &bridgeAuthTransport{
		base:  http.DefaultTransport,
		store: store,
		token: store.Create(bridgeUsername),
	}

	go func() {
		transport.refreshLoop(ctx, 10*time.Millisecond)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refreshLoop did not exit after ctx cancel")
	}
}

// TestBridgeAuthTransport_DoesNotMutateCallerRequest verifies that we clone
// before adding the header — the http.RoundTripper contract requires it.
func TestBridgeAuthTransport_DoesNotMutateCallerRequest(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	transport := newBridgeAuthTransport(ctx, http.DefaultTransport, store, time.Hour)

	req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
	require.NoError(t, err)
	require.Empty(t, req.Header.Get("Authorization"))

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Empty(t, req.Header.Get("Authorization"),
		"caller's request must not be mutated; transport should clone")
}
