package frontend

import (
	"net/http"
)

// handleTerminalWS upgrades the request to a WebSocket and hands it to the
// terminal manager. When s.term is nil (feature disabled in config), the
// route is reported as not found so it does not appear in the API surface.
//
// Authentication is enforced by NewFrontendAuthMiddleware before this
// handler is reached: any /api/* path requires a valid session when auth
// is enabled. After the upgrade, the WS lifetime is the session lifetime;
// no further auth checks are performed.
func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	if s.term == nil {
		http.NotFound(w, r)

		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Error().Err(err).Msg("terminal: websocket upgrade failed")

		return
	}
	defer conn.Close()

	_ = s.term.Run(r.Context(), conn)
}
