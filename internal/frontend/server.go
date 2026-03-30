package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1/commsv1connect"
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
	log      zerolog.Logger
	staticFS fs.FS
	hub      *ws.Hub
	cfg      *config.Config
}

// NewFrontendServer creates a new frontend Server that serves static assets and
// proxies mesh-status API calls to the openmanetd ConnectRPC backend.
func NewFrontendServer(ctx context.Context, cfg *config.Config, staticFS fs.FS) *Server {
	apiAddr := cfg.GetOpenMANETAPIAddress()

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

	go hub.Run()

	// Start the audio RX loop (receives from openmanetd, broadcasts to WS clients).
	b.StartAudioRXLoop(ctx)

	return &Server{
		log:      logger.GetLogger("frontend.server"),
		staticFS: staticFS,
		hub:      hub,
		cfg:      cfg,
	}
}

// Run starts the HTTP server and blocks until ctx is canceled.
// The caller is responsible for signal handling; Run reacts to ctx.Done()
// for graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	mux := s.handler()

	addr := net.JoinHostPort("", strconv.Itoa(s.cfg.GetOpenMANETWebsocketPort()))
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start the server in a goroutine.
	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.ListenAndServe()
	}()

	s.log.Info().Str("addr", addr).Msg("frontend server started")

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info().Msg("shutting down frontend server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}

		return nil
	}
}

// coiMiddleware adds Cross-Origin Isolation headers required for
// SharedArrayBuffer, which enables the AudioWorklet ring-buffer path
// for glitch-free audio playback.
func coiMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
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

	// Upgrade API endpoints.
	mux.HandleFunc("/api/upgrade/check", s.handleUpgradeCheck)
	mux.HandleFunc("/api/upgrade/download", s.handleUpgradeDownload)
	mux.HandleFunc("/api/upgrade/status", s.handleUpgradeStatus)
	mux.HandleFunc("/api/upgrade/apply", s.handleUpgradeApply)
	mux.HandleFunc("/api/upgrade/upload", s.handleUpgradeUpload)

	// SPA-aware static file server.
	// Serves static files if they exist, otherwise serves index.html
	// for client-side routing (React Router).
	staticFileServer := http.FileServerFS(s.staticFS)
	spaHandler := func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the static file directly.
		if r.URL.Path != "/" {
			filePath := strings.TrimPrefix(r.URL.Path, "/")
			if _, err := fs.Stat(s.staticFS, filePath); err == nil {
				staticFileServer.ServeHTTP(w, r)

				return
			}
		} else {
			staticFileServer.ServeHTTP(w, r)

			return
		}

		// Serve index.html for SPA client-side routing.
		indexFile, err := fs.ReadFile(s.staticFS, "index.html")
		if err != nil {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexFile)
	}

	// Register the SPA handler for root and all client-side routes.
	mux.HandleFunc("/", spaHandler)
	mux.HandleFunc("/settings", spaHandler)

	return coiMiddleware(mux)
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
