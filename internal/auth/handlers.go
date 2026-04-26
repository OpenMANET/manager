package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// AuthHandler implements the HTTP login, logout, change-password, and
// session-check endpoints. These are registered on the API server's mux
// alongside the ConnectRPC service handlers.
//
// Session cookies are written without the Secure attribute because the
// device is addressed over both http:// and https:// on the local LAN;
// setting Secure would cause browsers to drop the cookie over plain HTTP.
// Non-browser clients can authenticate via the Authorization: Bearer
// <token> header instead — see NewAPIAuthMiddleware.
type AuthHandler struct {
	Log            zerolog.Logger
	Authenticator  Authenticator
	Store          *SessionStore
	PasswordSetter PasswordSetter
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse is returned by POST /auth/login on success. Token mirrors the
// session cookie value so non-browser clients can capture it from the JSON
// body and send it as an Authorization: Bearer <token> header.
type loginResponse struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// checkResponse reports whether the caller has a valid session and whether
// the daemon has authentication enabled at all. Frontends hide password-
// management UI when AuthEnabled is false.
type checkResponse struct {
	Username      string `json:"username"`
	Authenticated bool   `json:"authenticated"`
	AuthEnabled   bool   `json:"authEnabled"`
}

// HandleLogin authenticates the user via PAM and issues a session cookie on
// success. Responds to POST /auth/login.
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})

		return
	}

	// Empty passwords are passed through to the Authenticator so PAM remains
	// the sole authority on credential policy (e.g. pam_unix nullok).
	if req.Username == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "username is required"})

		return
	}

	if err := h.Authenticator.Authenticate(req.Username, req.Password); err != nil {
		h.Log.Warn().Str("username", req.Username).Msg("authentication failed")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})

		return
	}

	token := h.Store.Create(req.Username)
	h.Log.Info().Str("username", req.Username).Msg("user logged in")

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(loginResponse{Username: req.Username, Token: token})
}

// HandleLogout deletes the active session and clears the session cookie.
// Reads the token from the session cookie or the Authorization: Bearer
// header, so non-browser clients can log out too. Responds to
// POST /auth/logout.
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	if token := extractSessionToken(r); token != "" {
		h.Store.Delete(token)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	w.WriteHeader(http.StatusNoContent)
}

// HandleCheck reports whether the request carries a valid session. Reads the
// token from the session cookie or the Authorization: Bearer header. Always
// returns HTTP 200 — the authenticated field indicates session state.
// Responds to GET /auth/check.
func (h *AuthHandler) HandleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	token := extractSessionToken(r)
	if token == "" {
		_ = json.NewEncoder(w).Encode(checkResponse{Authenticated: false, AuthEnabled: true})

		return
	}

	sess, ok := h.Store.Validate(token)
	if !ok {
		_ = json.NewEncoder(w).Encode(checkResponse{Authenticated: false, AuthEnabled: true})

		return
	}

	_ = json.NewEncoder(w).Encode(checkResponse{
		Authenticated: true,
		Username:      sess.Username,
		AuthEnabled:   true,
	})
}

// HandleChangePassword updates the caller's password. The caller must have a
// valid session (the auth middleware populates the context with their
// username). The current password is re-verified against the Authenticator to
// prevent session-hijack → password takeover. An empty currentPassword is
// passed through so first-time setup works when PAM is configured with
// nullok. Responds to POST /auth/change-password.
func (h *AuthHandler) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	username := UsernameFromContext(r.Context())
	if username == "" {
		writeUnauthorized(w)

		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})

		return
	}

	if req.NewPassword == "" {
		writeJSONError(w, http.StatusUnprocessableEntity, "new password is required")

		return
	}

	if strings.ContainsAny(req.NewPassword, "\n\r:") {
		writeJSONError(w, http.StatusUnprocessableEntity, "new password must not contain newline or colon")

		return
	}

	if err := h.Authenticator.Authenticate(username, req.CurrentPassword); err != nil {
		h.Log.Warn().Str("username", username).Msg("change-password: current password rejected")
		writeJSONError(w, http.StatusUnauthorized, "invalid credentials")

		return
	}

	if h.PasswordSetter == nil {
		h.Log.Error().Msg("change-password: no PasswordSetter configured")
		writeJSONError(w, http.StatusInternalServerError, "failed to change password")

		return
	}

	if err := h.PasswordSetter.SetPassword(r.Context(), username, req.NewPassword); err != nil {
		h.Log.Error().Err(err).Str("username", username).Msg("change-password: setter failed")

		if errors.Is(err, ErrInvalidPasswordInput) {
			writeJSONError(w, http.StatusUnprocessableEntity, "invalid password input")

			return
		}

		writeJSONError(w, http.StatusInternalServerError, "failed to change password")

		return
	}

	h.Log.Info().Str("username", username).Msg("user changed password")

	w.WriteHeader(http.StatusNoContent)
}

// HandleCheckDisabled is a standalone handler for GET /auth/check when
// authentication is disabled. It reports authenticated so the frontend skips
// the login gate, and sets AuthEnabled=false so the frontend hides UI that
// depends on a real session (e.g. password change).
func HandleCheckDisabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(checkResponse{
		Authenticated: true,
		Username:      "root",
		AuthEnabled:   false,
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
