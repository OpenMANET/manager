package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/auth"
	"github.com/stretchr/testify/assert"
)

func TestAPIAuthMiddleware_Disabled(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	mw := auth.NewAPIAuthMiddleware(store, false)

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/some/rpc", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestAPIAuthMiddleware_MissingCookie(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	mw := auth.NewAPIAuthMiddleware(store, true)

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/some.Service/Method", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAPIAuthMiddleware_ValidCookie(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	token := store.Create("alice")

	mw := auth.NewAPIAuthMiddleware(store, true)

	var gotUsername string

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUsername = auth.UsernameFromContext(r.Context())

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/some.Service/Method", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "alice", gotUsername)
}

func TestAPIAuthMiddleware_InvalidCookie(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	mw := auth.NewAPIAuthMiddleware(store, true)

	called := false
	handler := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/some.Service/Method", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "bad-token"})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAPIAuthMiddleware_SkipPaths(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	mw := auth.NewAPIAuthMiddleware(store, true)

	skipPaths := []string{
		"/auth/login",
		"/auth/check",
		"/openmanet.dashboard.v1.DashboardService/GetDashboardStatus",
	}

	for _, path := range skipPaths {
		t.Run(path, func(t *testing.T) {
			called := false
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true

				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodPost, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.True(t, called, "expected handler to be called for skip path %s", path)
			assert.Equal(t, http.StatusOK, rr.Code)
		})
	}
}

func TestFrontendAuthMiddleware_ProtectedPaths(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	mw := auth.NewFrontendAuthMiddleware(store, true)

	protectedPaths := []string{
		"/api/system/info",
		"/api/settings/config",
		"/ws",
	}

	for _, path := range protectedPaths {
		t.Run(path, func(t *testing.T) {
			called := false
			handler := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				called = true
			}))

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.False(t, called, "expected handler NOT to be called for protected path %s", path)
			assert.Equal(t, http.StatusUnauthorized, rr.Code)
		})
	}
}

func TestFrontendAuthMiddleware_UnprotectedPaths(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	mw := auth.NewFrontendAuthMiddleware(store, true)

	unprotectedPaths := []string{
		"/",
		"/login",
		"/settings",
		"/assets/main.js",
		"/favicon.ico",
	}

	for _, path := range unprotectedPaths {
		t.Run(path, func(t *testing.T) {
			called := false
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true

				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.True(t, called, "expected handler to be called for unprotected path %s", path)
		})
	}
}

func TestFrontendAuthMiddleware_ValidCookie(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	token := store.Create("bob")

	mw := auth.NewFrontendAuthMiddleware(store, true)

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/system/info", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rr.Code)
}
