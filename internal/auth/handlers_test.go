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

	h, _ := newTestAuthHandlerWithSetter(t, authErr)

	return h
}

// newTestAuthHandlerWithSetter returns an AuthHandler with a fakePasswordSetter
// so change-password tests can inspect the last call. The returned setter is
// the same instance installed on the handler.
func newTestAuthHandlerWithSetter(t *testing.T, authErr error) (*auth.AuthHandler, *fakePasswordSetter) {
	t.Helper()

	setter := &fakePasswordSetter{}

	return &auth.AuthHandler{
		Log:            zerolog.Nop(),
		Authenticator:  &fakeAuthenticator{err: authErr},
		Store:          auth.NewSessionStore(time.Hour, 16),
		PasswordSetter: setter,
	}, setter
}

// TestHandleLogin_Success verifies a valid login creates a session cookie and
// returns the token in the response body for non-browser clients.
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

	var resp struct {
		Username string `json:"username"`
		Token    string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "alice", resp.Username)
	assert.Equal(t, cookies[0].Value, resp.Token, "token in body must match session cookie")
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

// TestHandleLogin_MissingUsername verifies a 422 when the username is empty.
// Empty passwords are intentionally allowed through so PAM can decide (for
// pam_unix nullok or similar policies).
func TestHandleLogin_MissingUsername(t *testing.T) {
	h := newTestAuthHandler(t, nil)

	cases := []struct {
		name string
		body string
	}{
		{"empty username", `{"username":"","password":"secret"}`},
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

// TestHandleLogin_EmptyPassword verifies an empty password is delegated to
// the Authenticator rather than short-circuited at the handler.
func TestHandleLogin_EmptyPassword(t *testing.T) {
	h := newTestAuthHandler(t, nil) // Authenticator accepts → login succeeds

	body := `{"username":"alice","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.HandleLogin(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, rr.Result().Cookies(), 1)
}

// TestHandleLogin_EmptyPasswordRejectedByPAM verifies that when PAM rejects
// an empty password, the handler returns 401 rather than accepting it.
func TestHandleLogin_EmptyPasswordRejectedByPAM(t *testing.T) {
	h := newTestAuthHandler(t, errors.New("authentication failure"))

	body := `{"username":"alice","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.HandleLogin(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Empty(t, rr.Result().Cookies())
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
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:gosec // test fixture; production cookie security is verified separately

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
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:gosec // test fixture; production cookie security is verified separately

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
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:gosec // test fixture; production cookie security is verified separately

	rr := httptest.NewRecorder()
	h.HandleCheck(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp struct {
		Authenticated bool `json:"authenticated"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.False(t, resp.Authenticated)
}

// TestHandleCheck_Authenticated_AuthEnabledTrue verifies the enabled-path check
// response includes authEnabled: true so frontends know password-change UI is
// available.
func TestHandleCheck_Authenticated_AuthEnabledTrue(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	token := store.Create("alice")

	h := &auth.AuthHandler{Log: zerolog.Nop(), Store: store}

	req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:gosec // test fixture; production cookie security is verified separately

	rr := httptest.NewRecorder()
	h.HandleCheck(rr, req)

	var resp struct {
		Authenticated bool `json:"authenticated"`
		AuthEnabled   bool `json:"authEnabled"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.True(t, resp.Authenticated)
	assert.True(t, resp.AuthEnabled)
}

// TestHandleCheckDisabled_AuthEnabledFalse verifies the disabled-path handler
// reports authEnabled: false so frontends hide session-dependent UI.
func TestHandleCheckDisabled_AuthEnabledFalse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	rr := httptest.NewRecorder()
	auth.HandleCheckDisabled(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp struct {
		Authenticated bool `json:"authenticated"`
		AuthEnabled   bool `json:"authEnabled"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.True(t, resp.Authenticated)
	assert.False(t, resp.AuthEnabled)
}

// newAuthdChangePasswordReq builds a POST /auth/change-password request with
// the given username already baked into the request context — mirroring what
// the auth middleware does on a real request.
func newAuthdChangePasswordReq(username, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/change-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if username != "" {
		req = req.WithContext(auth.ContextWithUsername(req.Context(), username))
	}

	return req
}

// TestHandleChangePassword_Success verifies the happy path: a valid session
// plus correct current password invokes the setter and returns 204.
func TestHandleChangePassword_Success(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, nil)

	req := newAuthdChangePasswordReq("alice", `{"currentPassword":"old","newPassword":"new"}`)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Equal(t, 1, setter.Calls())
	assert.Equal(t, "alice", setter.GotUser())
	assert.Equal(t, "new", setter.GotPass())
}

// TestHandleChangePassword_EmptyCurrentPassword_NullOk verifies the first-time
// setup flow: empty current password delegates to PAM nullok; a passing
// authenticator means the setter runs.
func TestHandleChangePassword_EmptyCurrentPassword_NullOk(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, nil)

	req := newAuthdChangePasswordReq("root", `{"currentPassword":"","newPassword":"alpha123"}`)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Equal(t, 1, setter.Calls())
	assert.Equal(t, "alpha123", setter.GotPass())
}

func TestHandleChangePassword_WrongMethod(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/change-password", nil)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	assert.Equal(t, 0, setter.Calls())
}

func TestHandleChangePassword_NoSession(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, nil)

	// No username injected into context — middleware would have already
	// rejected, but the handler defends in depth.
	req := newAuthdChangePasswordReq("", `{"currentPassword":"old","newPassword":"new"}`)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Equal(t, 0, setter.Calls())
}

func TestHandleChangePassword_BadJSON(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, nil)

	req := newAuthdChangePasswordReq("alice", `{not json`)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, 0, setter.Calls())
}

func TestHandleChangePassword_EmptyNewPassword(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, nil)

	req := newAuthdChangePasswordReq("alice", `{"currentPassword":"old","newPassword":""}`)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
	assert.Equal(t, 0, setter.Calls())
}

func TestHandleChangePassword_InvalidNewPasswordChars(t *testing.T) {
	cases := []struct {
		name string
		pw   string
	}{
		{"newline", "bad\npw"},
		{"carriage return", "bad\rpw"},
		{"colon", "bad:pw"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, setter := newTestAuthHandlerWithSetter(t, nil)

			body, err := json.Marshal(map[string]string{
				"currentPassword": "old",
				"newPassword":     tc.pw,
			})
			require.NoError(t, err)

			req := newAuthdChangePasswordReq("alice", string(body))
			rr := httptest.NewRecorder()
			h.HandleChangePassword(rr, req)

			assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
			assert.Equal(t, 0, setter.Calls(), "setter must not be invoked for invalid input")
		})
	}
}

func TestHandleChangePassword_WrongCurrentPassword(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, errors.New("authentication failure"))

	req := newAuthdChangePasswordReq("alice", `{"currentPassword":"wrong","newPassword":"new"}`)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Equal(t, 0, setter.Calls(), "setter must not be invoked when current password is rejected")
}

func TestHandleChangePassword_SetterFails(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, nil)
	setter.SetErr(errors.New("chpasswd: exit 1"))

	req := newAuthdChangePasswordReq("alice", `{"currentPassword":"old","newPassword":"new"}`)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, 1, setter.Calls())

	var resp struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Contains(t, resp.Error, "failed to change password")
}

func TestHandleChangePassword_SetterInvalidInput(t *testing.T) {
	h, setter := newTestAuthHandlerWithSetter(t, nil)
	setter.SetErr(auth.ErrInvalidPasswordInput)

	req := newAuthdChangePasswordReq("alice", `{"currentPassword":"old","newPassword":"new"}`)
	rr := httptest.NewRecorder()
	h.HandleChangePassword(rr, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
	assert.Equal(t, 1, setter.Calls())
}

// TestHandleLogout_BearerHeader verifies logout via Bearer header invalidates
// the session, even without a cookie on the request.
func TestHandleLogout_BearerHeader(t *testing.T) {
	store := auth.NewSessionStore(time.Hour, 16)
	token := store.Create("alice")

	h := &auth.AuthHandler{Log: zerolog.Nop(), Store: store}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr := httptest.NewRecorder()
	h.HandleLogout(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)

	_, ok := store.Validate(token)
	assert.False(t, ok, "session must be deleted when logout uses a Bearer token")
}
