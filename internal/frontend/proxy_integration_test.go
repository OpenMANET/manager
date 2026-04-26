package frontend

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/auth"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAuth always succeeds; the test exercises the proxy + middleware
// plumbing, not the PAM stack.
type stubAuth struct{}

func (stubAuth) Authenticate(_, _ string) error { return nil }

// Build a complete auth-enabled API server that mirrors how the daemon
// wires up its handlers. Uses an httptest.Server for plain-HTTP loopback
// (matches the real DefaultOpenMANETCommsAPIAddress: http://127.0.0.1:8087).
func newAuthEnabledAPIServer(t *testing.T) (*httptest.Server, *auth.SessionStore) {
	t.Helper()

	store := auth.NewSessionStore(time.Hour, 16)

	authHandler := &auth.AuthHandler{
		Log:           zerolog.Nop(),
		Authenticator: stubAuth{},
		Store:         store,
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/auth/login", authHandler.HandleLogin)
	apiMux.HandleFunc("/auth/logout", authHandler.HandleLogout)
	apiMux.HandleFunc("/auth/check", authHandler.HandleCheck)

	// A protected ConnectRPC-shaped endpoint to confirm cookie/Bearer
	// round-trips via the proxy.
	apiMux.HandleFunc("/openmanet.foo.v1.FooService/Method", func(w http.ResponseWriter, r *http.Request) {
		if u := auth.UsernameFromContext(r.Context()); u != "" {
			w.Header().Set("X-Authenticated-User", u)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	authMW := auth.NewAPIAuthMiddleware(store, true)

	srv := httptest.NewServer(authMW(apiMux))
	t.Cleanup(srv.Close)

	return srv, store
}

// Build the frontend server's full handler chain (COI middleware + proxies +
// auth middleware + SPA), then wrap it in an httptest TLS server so the test
// drives the same code path a browser does over HTTPS.
func newFrontendTLSServer(t *testing.T, apiAddr string) *httptest.Server {
	t.Helper()

	rpcProxy, authProxy := buildAPIProxies(apiAddr, zerolog.Nop())
	require.NotNil(t, rpcProxy)
	require.NotNil(t, authProxy)

	srv := newTestServer(func(s *Server) {
		s.rpcProxy = rpcProxy
		s.authProxy = authProxy
	})

	ts := httptest.NewTLSServer(srv.handler())
	t.Cleanup(ts.Close)

	return ts
}

// httpsClient returns a client that:
//   - trusts the test TLS cert
//   - has a cookie jar (so a Set-Cookie on /auth/login is replayed on /rpc/...)
func httpsClient(t *testing.T, ts *httptest.Server) *http.Client {
	t.Helper()

	jar, err := newTestJar()
	require.NoError(t, err)

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
		Jar:     jar,
		Timeout: 10 * time.Second,
	}
}

// TestProxyAuthFlow_HTTPSCookieRoundTrip drives the exact sequence the
// browser uses over HTTPS: GET /auth/check (skip-listed), POST /auth/login
// (skip-listed, sets cookie), POST /rpc/<service>/<method> (must succeed
// because the proxy carries the cookie to the upstream).
//
// This is the smoke test for the "every endpoint returns 401 over HTTPS"
// regression — if the cookie is dropped anywhere along the chain, this
// test fails.
func TestProxyAuthFlow_HTTPSCookieRoundTrip(t *testing.T) {
	api, store := newAuthEnabledAPIServer(t)
	front := newFrontendTLSServer(t, api.URL)
	client := httpsClient(t, front)

	// 1. /auth/check — anonymous probe, must reach upstream and report not authenticated.
	resp, err := client.Get(front.URL + "/auth/check")
	require.NoError(t, err)

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "check body=%s", body)
	assert.Contains(t, string(body), `"authenticated":false`)

	// 2. /auth/login — must succeed and set the session cookie.
	resp, err = client.Post(front.URL+"/auth/login", "application/json",
		strings.NewReader(`{"username":"root","password":""}`))
	require.NoError(t, err)

	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "login body=%s", body)

	// The Set-Cookie must have flowed through the proxy to the cookie jar.
	frontURL, _ := url.Parse(front.URL)
	cookies := client.Jar.Cookies(frontURL)
	require.Len(t, cookies, 1, "expected exactly one cookie after login, got %d", len(cookies))
	assert.Equal(t, auth.SessionCookieName, cookies[0].Name)
	assert.NotEmpty(t, cookies[0].Value)

	// And the session must exist in the upstream store.
	_, ok := store.Validate(cookies[0].Value)
	assert.True(t, ok, "session token returned by proxy is not present in upstream store")

	// 3. /rpc/<service>/<method> — protected endpoint, must succeed because
	// the cookie jar replays the session cookie and the proxy forwards it.
	resp, err = client.Post(front.URL+"/rpc/openmanet.foo.v1.FooService/Method",
		"application/json", strings.NewReader(`{}`))
	require.NoError(t, err)

	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "rpc body=%s", body)
	assert.Equal(t, "root", resp.Header.Get("X-Authenticated-User"),
		"upstream did not see the authenticated session — cookie was not forwarded")
}

func TestProxyAuthFlow_HTTPSBearerToken(t *testing.T) {
	api, _ := newAuthEnabledAPIServer(t)
	front := newFrontendTLSServer(t, api.URL)
	client := httpsClient(t, front)

	// Login to mint a token.
	resp, err := client.Post(front.URL+"/auth/login", "application/json",
		strings.NewReader(`{"username":"alice","password":""}`))
	require.NoError(t, err)

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "login body=%s", body)

	// Capture the token from the JSON body — same path an external Bearer
	// client would take.
	tokenStart := strings.Index(string(body), `"token":"`) + len(`"token":"`)
	tokenEnd := strings.Index(string(body)[tokenStart:], `"`)
	token := string(body)[tokenStart : tokenStart+tokenEnd]
	require.NotEmpty(t, token)

	// Drop the cookie from the jar to prove the Bearer path works on its own.
	client.Jar, _ = newTestJar()

	req, err := http.NewRequest(http.MethodPost,
		front.URL+"/rpc/openmanet.foo.v1.FooService/Method",
		strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "alice", resp.Header.Get("X-Authenticated-User"))
}

// Minimal cookie jar: net/http/cookiejar requires a PublicSuffix list which
// rejects raw IPs and localhost in some cases. We need same-host policy only.
type testJar struct {
	cookies map[string][]*http.Cookie
}

func newTestJar() (*testJar, error) {
	return &testJar{cookies: make(map[string][]*http.Cookie)}, nil
}

func (j *testJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}

	host := u.Host
	merged := make([]*http.Cookie, 0, len(j.cookies[host])+len(cookies))

	// Drop any prior cookie with the same name.
	for _, existing := range j.cookies[host] {
		keep := true

		for _, incoming := range cookies {
			if existing.Name == incoming.Name {
				keep = false

				break
			}
		}

		if keep {
			merged = append(merged, existing)
		}
	}

	merged = append(merged, cookies...)
	j.cookies[host] = merged
}

func (j *testJar) Cookies(u *url.URL) []*http.Cookie {
	return j.cookies[u.Host]
}

var _ = errors.New // keep errors imported for future cookie validation
