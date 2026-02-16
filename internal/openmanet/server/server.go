package server

import (
	"context"
	"net/http"
	"time"

	connectcors "connectrpc.com/cors"
	services "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
)

const (
	serverAddress = "0.0.0.0:8087"
)

type APIServer struct {
	Cfg       *config.Config
	Log       zerolog.Logger
	DB        *models.Queries
	ApiServer *http.Server
	Wifi      *mgmt.WirelessConfig
	GPS       *gpsd.GPSService
}

func NewAPIServer(cfg APIServer) *APIServer {
	api := http.NewServeMux()

	api.Handle(services.NewNodeServiceHandler(&handlers.NodeService{
		DB:  cfg.DB,
		Log: cfg.Log,
	}))

	api.Handle(services.NewInterfaceServiceHandler(&handlers.InterfaceService{
		Log:  cfg.Log,
		Wifi: cfg.Wifi,
	}))

	api.Handle(services.NewMeshNeighborServiceHandler(&handlers.MeshService{
		Log:  cfg.Log,
		Wifi: cfg.Wifi,
	}))

	api.Handle(services.NewStatusServiceHandler(&handlers.StatusService{
		Cfg:  cfg.Cfg,
		Log:  cfg.Log,
		Wifi: cfg.Wifi,
		GPS:  cfg.GPS,
	}))

	p := new(http.Protocols)
	p.SetHTTP1(true)
	// Use h2c so we can serve HTTP/2 without TLS.
	p.SetUnencryptedHTTP2(true)
	server := http.Server{
		Addr:         serverAddress,
		Handler:      withCORS(api),
		Protocols:    p,
		ReadTimeout:  time.Duration(30 * time.Second),
		WriteTimeout: time.Duration(30 * time.Second),
		ErrorLog:     logger.StandardLogger(cfg.Log),
	}

	return &APIServer{
		Log:       cfg.Log,
		DB:        cfg.DB,
		ApiServer: &server,
	}
}

func (s *APIServer) Stop(ctx context.Context) error {
	return s.ApiServer.Shutdown(ctx)
}

func withCORS(handler http.Handler) http.Handler {
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"}, // Allow all origins for private network
		AllowedMethods: connectcors.AllowedMethods(),
		AllowedHeaders: append(connectcors.AllowedHeaders(), "Access-Control-Request-Private-Network"),
		ExposedHeaders: connectcors.ExposedHeaders(),
		// Crucial for PNA:
		OptionsPassthrough: false,
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Handle the Vary header for caching safety
		w.Header().Add("Vary", "Access-Control-Request-Private-Network")

		// 2. If it's a PNA preflight request, allow it
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}

		c.Handler(handler).ServeHTTP(w, r)
	})
}
