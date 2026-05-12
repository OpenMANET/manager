package frontend

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/openmanet/openmanetd/internal/auth"
)

// bridgeUsername is the synthetic operator name attached to the session
// the audio bridge uses to authenticate its in-process RPC calls. The
// double underscores make it obvious in audit logs that the session is
// daemon-internal and not a real human login.
const bridgeUsername = "__bridge__"

// bridgeAuthTransport wraps an http.RoundTripper and attaches a session
// Bearer token to every outbound request. The token is minted from the
// in-process SessionStore at construction and refreshed on a schedule
// so it never expires under a long-running stream.
//
// This exists because the audio bridge runs in the frontend daemon but
// the CommsService it drives is on the API daemon. When auth is enabled
// the API auth middleware rejects unauthenticated calls, so the bridge
// needs credentials. Sharing the SessionStore in-process lets the bridge
// mint a session without going through PAM.
type bridgeAuthTransport struct {
	base  http.RoundTripper
	store *auth.SessionStore
	token string
	mu    sync.RWMutex
}

// newBridgeAuthTransport creates a transport that adds Bearer auth and
// starts a background goroutine that mints a fresh token at refreshInterval
// until ctx is canceled. Old tokens remain valid until their TTL expires,
// so requests in flight with a stale token complete normally.
func newBridgeAuthTransport(ctx context.Context, base http.RoundTripper, store *auth.SessionStore, refreshInterval time.Duration) *bridgeAuthTransport {
	t := &bridgeAuthTransport{
		base:  base,
		store: store,
		token: store.Create(bridgeUsername),
	}

	go t.refreshLoop(ctx, refreshInterval)

	return t
}

func (t *bridgeAuthTransport) refreshLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fresh := t.store.Create(bridgeUsername)

			t.mu.Lock()
			t.token = fresh
			t.mu.Unlock()
		}
	}
}

// RoundTrip clones the request before mutating it, per the http.RoundTripper
// contract, then delegates to the wrapped transport.
func (t *bridgeAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	token := t.token
	t.mu.RUnlock()

	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+token)

	resp, err := t.base.RoundTrip(cloned)
	if err != nil {
		return resp, fmt.Errorf("bridge auth round trip: %w", err)
	}

	return resp, nil
}
