package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// AuthHandler implements the HTTP login, logout, and session-check endpoints.
// These are registered on the API server's mux alongside the ConnectRPC
// service handlers.
type AuthHandler struct {
	Log           zerolog.Logger
	Authenticator Authenticator
	Store         *SessionStore
	// CookieSecure controls the Secure attribute on the session cookie.
	// Set to true when the frontend is served over HTTPS.
	CookieSecure bool
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type checkResponse struct {
	Username      string `json:"username"`
	Authenticated bool   `json:"authenticated"`
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
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"username": req.Username})
}

// HandleLogout deletes the active session and clears the session cookie.
// Responds to POST /auth/logout.
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		h.Store.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	w.WriteHeader(http.StatusNoContent)
}

// HandleCheck reports whether the request carries a valid session cookie.
// Always returns HTTP 200 — the authenticated field indicates session state.
// Responds to GET /auth/check.
func (h *AuthHandler) HandleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		_ = json.NewEncoder(w).Encode(checkResponse{Authenticated: false})

		return
	}

	sess, ok := h.Store.Validate(cookie.Value)
	if !ok {
		_ = json.NewEncoder(w).Encode(checkResponse{Authenticated: false})

		return
	}

	_ = json.NewEncoder(w).Encode(checkResponse{Authenticated: true, Username: sess.Username})
}

// HandleCheckDisabled is a standalone handler for GET /auth/check when
// authentication is disabled. It always reports authenticated so the
// frontend skips the login gate.
func HandleCheckDisabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(checkResponse{Authenticated: true, Username: "admin"})
}
