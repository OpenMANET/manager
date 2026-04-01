package auth

import (
	"context"
	"encoding/json"
	"net/http"
)

// ctxKey is an unexported type for context keys in this package.
type ctxKey int

const (
	ctxKeyUsername ctxKey = iota
)

// SessionCookieName is the name of the HTTP session cookie.
const SessionCookieName = "session"

// NewAPIAuthMiddleware returns HTTP middleware that enforces session
// authentication for the ConnectRPC API server. When enabled is false the
// middleware is a no-op. The following paths are always allowed without a
// valid session:
//   - POST /auth/login
//   - GET  /auth/check
//   - POST /openmanet.dashboard.v1.DashboardService/GetDashboardStatus
func NewAPIAuthMiddleware(store *SessionStore, enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled || isAPISkipPath(r) {
				next.ServeHTTP(w, r)

				return
			}

			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				writeUnauthorized(w)

				return
			}

			sess, ok := store.Validate(cookie.Value)
			if !ok {
				writeUnauthorized(w)

				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUsername, sess.Username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// NewFrontendAuthMiddleware returns HTTP middleware that enforces session
// authentication for the frontend server. Only requests to /api/* and /ws
// require a valid session. All other paths (SPA shell, static assets) pass
// through. When enabled is false the middleware is a no-op.
func NewFrontendAuthMiddleware(store *SessionStore, enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled || !isFrontendProtectedPath(r) {
				next.ServeHTTP(w, r)

				return
			}

			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				writeUnauthorized(w)

				return
			}

			sess, ok := store.Validate(cookie.Value)
			if !ok {
				writeUnauthorized(w)

				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUsername, sess.Username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UsernameFromContext returns the authenticated username stored in ctx, or an
// empty string if no session is present (e.g. when auth is disabled).
func UsernameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUsername).(string)

	return v
}

// isAPISkipPath returns true for paths that are always allowed on the API
// server without authentication.
func isAPISkipPath(r *http.Request) bool {
	switch r.URL.Path {
	case "/auth/login", "/auth/check":
		return true
	case "/openmanet.dashboard.v1.DashboardService/GetDashboardStatus":
		return true
	}

	return false
}

// isFrontendProtectedPath returns true for frontend server paths that require
// a valid session.
func isFrontendProtectedPath(r *http.Request) bool {
	p := r.URL.Path
	if len(p) >= 5 && p[:5] == "/api/" {
		return true
	}

	return p == "/ws"
}

// writeUnauthorized sends a 401 JSON response.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}
