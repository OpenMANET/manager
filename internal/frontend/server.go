package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1/commsv1connect"
	"github.com/openmanet/openmanetd/internal/auth"
	"github.com/openmanet/openmanetd/internal/bridge"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/util/logger"
	ws "github.com/openmanet/openmanetd/internal/websocket"
	"github.com/rs/zerolog"
)

const (
	shutdownTimeout = 10 * time.Second
)

var upgrader = websocket.Upgrader{ //nolint:gochecknoglobals
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Server is the HTTP/WebSocket server for the WebUI.
type Server struct {
	log          zerolog.Logger
	staticFS     fs.FS
	hub          *ws.Hub
	cfg          *config.Config
	sessionStore *auth.SessionStore
	indexHTML    []byte
	authEnabled  bool
}

// NewFrontendServer creates a new frontend Server that serves static assets and
// proxies mesh-status API calls to the openmanetd ConnectRPC backend.
// sessionStore may be nil when auth is disabled.
func NewFrontendServer(ctx context.Context, cfg *config.Config, staticFS fs.FS, sessionStore *auth.SessionStore, authEnabled bool) *Server {
	apiAddr := cfg.GetOpenMANETCommsAPIAddress()

	// Create openmanetd RPC client with a dial timeout so streaming
	// RPCs don't hang indefinitely when openmanetd is unreachable.
	rpcHTTPClient := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 10 * time.Second,
			}).DialContext,
		},
	}

	commsClient := commsv1connect.NewCommsServiceClient(rpcHTTPClient, apiAddr)

	// Create bridge and hub.
	var b *bridge.Bridge

	hub := ws.NewHub(func(client *ws.Client, data []byte) {
		b.HandleMessage(client, data)
	})
	b = bridge.NewBridge(hub, commsClient)

	go hub.Run(ctx)

	// Start the audio RX loop (receives from openmanetd, broadcasts to WS clients).
	b.StartAudioRXLoop(ctx)

	log := logger.GetLogger("frontend.server")

	indexHTML, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		log.Warn().Err(err).Msg("failed to pre-read index.html; SPA fallback will be unavailable")
	}

	return &Server{
		log:          log,
		staticFS:     staticFS,
		hub:          hub,
		cfg:          cfg,
		indexHTML:    indexHTML,
		sessionStore: sessionStore,
		authEnabled:  authEnabled,
	}
}

// Run starts the HTTP and TLS servers and blocks until ctx is canceled.
// The caller is responsible for signal handling; Run reacts to ctx.Done()
// for graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	mux := s.handler()

	// ── HTTP server (unchanged) ─────────────────────────────────────────
	addr := s.cfg.GetOpenMANETFrontendHostPort()

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64 KB
	}

	errCh := make(chan error, 2) //nolint:mnd

	go func() {
		errCh <- srv.ListenAndServe()
	}()

	s.log.Info().Str("addr", addr).Msg("frontend HTTP server started")

	// ── TLS server ──────────────────────────────────────────────────────
	certFile := s.cfg.GetOpenMANETFrontendTLSCertFile()
	keyFile := s.cfg.GetOpenMANETFrontendTLSKeyFile()

	tlsConfig, err := buildTLSConfig(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("frontend TLS config: %w", err)
	}

	tlsAddr := s.cfg.GetOpenMANETFrontendTLSHostPort()

	tlsSrv := &http.Server{
		Addr:              tlsAddr,
		Handler:           mux,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64 KB
	}

	go func() {
		// Empty cert/key paths because the certificate is already in TLSConfig.
		errCh <- tlsSrv.ListenAndServeTLS("", "")
	}()

	selfSigned := certFile == ""
	s.log.Info().Str("addr", tlsAddr).Bool("self-signed", selfSigned).Msg("frontend TLS server started")

	// ── Wait for error or context cancellation ──────────────────────────
	select {
	case err := <-errCh:
		// One server failed; shut down the other.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		_ = srv.Shutdown(shutdownCtx)
		_ = tlsSrv.Shutdown(shutdownCtx)

		return err
	case <-ctx.Done():
		s.log.Info().Msg("shutting down frontend servers")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		httpErr := srv.Shutdown(shutdownCtx)
		tlsErr := tlsSrv.Shutdown(shutdownCtx)

		return errors.Join(httpErr, tlsErr)
	}
}

// coiMiddleware adds Cross-Origin Isolation headers required for
// SharedArrayBuffer, which enables the AudioWorklet ring-buffer path
// for glitch-free audio playback.
func coiMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		w.Header().Set("Permissions-Policy", "microphone=*, speaker-selection=*")
		next.ServeHTTP(w, r)
	})
}

// handler builds the HTTP mux with all routes registered.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()

	// WebSocket endpoint.
	mux.HandleFunc("/ws", s.handleWebSocket)

	// System management API endpoints.
	mux.HandleFunc("/api/system/info", s.handleSystemInfo)
	mux.HandleFunc("/api/system/processes", s.handleSystemProcesses)
	mux.HandleFunc("/api/system/storage", s.handleSystemStorage)
	mux.HandleFunc("/api/system/logs", s.handleSystemLogs)
	mux.HandleFunc("/api/system/reboot", s.handleSystemReboot)
	mux.HandleFunc("/api/system/restart-service", s.handleSystemRestartService)

	// Network API endpoints.
	mux.HandleFunc("/api/network/interfaces", s.handleNetworkInterfaces)
	mux.HandleFunc("/api/network/wifi", s.handleNetworkWifi)
	mux.HandleFunc("/api/network/routes", s.handleNetworkRoutes)
	mux.HandleFunc("/api/network/batman", s.handleNetworkBatman)

	// Settings API endpoints.
	mux.HandleFunc("/api/settings/config", s.handleSettingsConfig)
	mux.HandleFunc("/api/settings/hostname", s.handleSettingsHostname)

	// Whisper management endpoints.
	mux.HandleFunc("/api/whisper/status", s.handleWhisperStatus)
	mux.HandleFunc("/api/whisper/download", s.handleWhisperDownload)
	mux.HandleFunc("/api/whisper/download/status", s.handleWhisperDownloadStatus)
	mux.HandleFunc("/api/whisper/remove", s.handleWhisperRemove)

	// SPA-aware static file server.
	// Serves static files if they exist, otherwise serves index.html
	// for client-side routing (React Router).
	staticFileServer := http.FileServerFS(s.staticFS)
	spaHandler := func(w http.ResponseWriter, r *http.Request) {
		// Serve whisper model files from the runtime download directory
		// (/tmp/whisper by default) before checking the embedded staticFS.
		// The WASM JS file remains embedded; only the large model is optional.
		if strings.HasPrefix(r.URL.Path, "/whisper/") {
			name := filepath.Base(r.URL.Path)
			candidate := filepath.Join(whisperDir, name)

			absCandidate, err := filepath.Abs(candidate)
			if err == nil && strings.HasPrefix(absCandidate, whisperDir) {
				if _, statErr := os.Stat(absCandidate); statErr == nil {
					http.ServeFile(w, r, absCandidate)

					return
				}
			}

			// Fall through to embedded staticFS.
		}

		// Try to serve the static file directly.
		if r.URL.Path != "/" {
			fp := strings.TrimPrefix(r.URL.Path, "/")
			if _, err := fs.Stat(s.staticFS, fp); err == nil {
				staticFileServer.ServeHTTP(w, r)

				return
			}
		} else {
			staticFileServer.ServeHTTP(w, r)

			return
		}

		// Serve cached index.html for SPA client-side routing.
		if s.indexHTML == nil {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(s.indexHTML)
	}

	// Register the SPA handler for root and all client-side routes.
	mux.HandleFunc("/", spaHandler)
	mux.HandleFunc("/settings", spaHandler)
	mux.HandleFunc("/login", spaHandler)

	authMW := auth.NewFrontendAuthMiddleware(s.sessionStore, s.authEnabled)

	return coiMiddleware(authMW(mux))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Error().Err(err).Msg("websocket upgrade failed")

		return
	}

	client := ws.NewClient(s.hub, conn)
	s.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}

// writeJSON encodes v as JSON and writes it to the response.
func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error().Err(err).Msg("json encode failed")
	}
}

// writeError writes an error JSON response with a 502 Bad Gateway status.
func (s *Server) writeError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)

	_ = json.NewEncoder(w).Encode(errorResponse{Error: msg})
}

type errorResponse struct {
	Error string `json:"error"`
}
