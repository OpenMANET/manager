package auth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/auth"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAuthenticator is a hand-rolled fake implementing auth.Authenticator.
type fakeAuthenticator struct {
	err error
}

func (f *fakeAuthenticator) Authenticate(_, _ string) error {
	return f.err
}

func newTestAuthHandler(t *testing.T, authErr error) *auth.AuthHandler {
	t.Helper()

	return &auth.AuthHandler{
		Log:           zerolog.Nop(),
		Authenticator: &fakeAuthenticator{err: authErr},
		Store:         auth.NewSessionStore(time.Hour, 16),
		CookieSecure:  false,
	}
}

// TestHandleLogin_Success verifies a valid login creates a session cookie.
func TestHandleLogin_Success(t *testing.T) {
	h := newTestAuthHandler(t, nil)

	body := `{"username":"alice","password":"secret"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	h.HandleLogin(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	cookies := rr.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, auth.SessionCookieName, cookies[0].Name)
	assert.NotEmpty(t, cookies[0].Value)
	assert.True(t, cookies[0].HttpOnly)
}

// TestHandleLogin_InvalidCredentials verifies a 401 on auth failure.
func TestHandleLogin_InvalidCredentials(t *testing.T) {
	h := newTestAuthHandler(t, errors.New("bad password"))

	body := `{"username":"alice","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	h.HandleLogin(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Empty(t, rr.Result().Cookies())
}

// TestHandleLogin_MissingFields verifies a 422 when fields are omitted.
func TestHandleLogin_MissingFields(t *testing.T) {
	h := newTestAuthHandler(t, nil)

	cases := []struct {
		name string
		body string
	}{
		{"empty username", `{"username":"","password":"secret"}`},
		{"empty password", `{"username":"alice","password":""}`},
		{"both empty", `{"username":"","password":""}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(tc.body))
			rr := httptest.NewRecorder()
			h.HandleLogin(rr, req)
			assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
		})
	}
}

// TestHandleLogin_WrongMethod verifies non-POST is rejected.
func TestHandleLogin_WrongMethod(t *testing.T) {
	h := newTestAuthHandler(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rr := httptest.NewRecorder()
	h.HandleLogin(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

// TestHandleLogout_ClearsSession verifies logout deletes the session and clears the cookie.
func TestHandleLogout_ClearsSession(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	token := store.Create("alice")

	h := &auth.AuthHandler{
		Log:   zerolog.Nop(),
		Store: store,
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})

	rr := httptest.NewRecorder()
	h.HandleLogout(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)

	// Session must be gone.
	_, ok := store.Validate(token)
	assert.False(t, ok)

	// Cookie must be cleared (MaxAge <= 0).
	cookies := rr.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.LessOrEqual(t, cookies[0].MaxAge, 0)
}

// TestHandleLogout_NoCookie verifies logout succeeds even without a cookie.
func TestHandleLogout_NoCookie(t *testing.T) {
	h := newTestAuthHandler(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rr := httptest.NewRecorder()
	h.HandleLogout(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
}

// TestHandleCheck_Authenticated verifies check returns true for a valid session.
func TestHandleCheck_Authenticated(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	token := store.Create("alice")

	h := &auth.AuthHandler{
		Log:   zerolog.Nop(),
		Store: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})

	rr := httptest.NewRecorder()
	h.HandleCheck(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.True(t, resp.Authenticated)
	assert.Equal(t, "alice", resp.Username)
}

// TestHandleCheck_Unauthenticated verifies check returns false without a session.
func TestHandleCheck_Unauthenticated(t *testing.T) {
	h := newTestAuthHandler(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	rr := httptest.NewRecorder()
	h.HandleCheck(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp struct {
		Authenticated bool `json:"authenticated"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.False(t, resp.Authenticated)
}

// TestHandleCheck_ExpiredSession verifies check returns false for an expired session.
func TestHandleCheck_ExpiredSession(t *testing.T) {
	store := auth.NewSessionStore(time.Millisecond, 16)
	token := store.Create("alice")

	time.Sleep(5 * time.Millisecond)

	h := &auth.AuthHandler{
		Log:   zerolog.Nop(),
		Store: store,
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})

	rr := httptest.NewRecorder()
	h.HandleCheck(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp struct {
		Authenticated bool `json:"authenticated"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.False(t, resp.Authenticated)
}
