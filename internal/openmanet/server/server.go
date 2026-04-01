package server

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	connectcors "connectrpc.com/cors"
	"connectrpc.com/validate"
	blosconnect "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1/blosv1connect"
	commsconnect "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1/commsv1connect"
	dashboardconnect "github.com/openmanet/openmanetd/internal/api/openmanet/dashboard/v1/dashboardv1connect"
	niconnect "github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1/network_interfacev1connect"
	services "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	wificonfigconnect "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1/wifi_configv1connect"
	"github.com/openmanet/openmanetd/internal/auth"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/iwinfo"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/openmanet/openmanetd/internal/system"
	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
)

type APIServer struct {
	Cfg           *config.Config
	Log           zerolog.Logger
	DB            *models.Queries
	ApiServer     *http.Server
	Wifi          *mgmt.WirelessConfig
	GPS           *gpsd.GPSService
	BLOSManager   blos.BLOSLifecycle
	CommsManager  comms.CommsLifecycle
	Interfaces    handlers.InterfaceProvider
	DHCP          handlers.DHCPConfigProvider
	Leases        handlers.LeaseProvider
	Tailscale     handlers.TailscaleStatusProvider
	SessionStore  *auth.SessionStore
	Authenticator auth.Authenticator
	AuthEnabled   bool
}

func NewAPIServer(cfg APIServer) *APIServer {
	var (
		api                 = http.NewServeMux()
		validateInterceptor = validate.NewInterceptor()
	)

	api.Handle(services.NewNodeServiceHandler(&handlers.NodeService{
		DB:  cfg.DB,
		Log: cfg.Log,
	}, connect.WithInterceptors(validateInterceptor)))

	api.Handle(services.NewInterfaceServiceHandler(&handlers.InterfaceService{
		Log:  cfg.Log,
		Wifi: cfg.Wifi,
	}, connect.WithInterceptors(validateInterceptor)))

	api.Handle(services.NewMeshNeighborServiceHandler(&handlers.MeshService{
		Log:  cfg.Log,
		Wifi: cfg.Wifi,
	}, connect.WithInterceptors(validateInterceptor)))

	api.Handle(services.NewStatusServiceHandler(&handlers.StatusService{
		Cfg:  cfg.Cfg,
		Log:  cfg.Log,
		Wifi: cfg.Wifi,
		GPS:  cfg.GPS,
	}, connect.WithInterceptors(validateInterceptor)))

	api.Handle(commsconnect.NewCommsServiceHandler(&handlers.CommsService{
		Cfg:          cfg.Cfg,
		Log:          cfg.Log,
		CommsManager: cfg.CommsManager,
	}, connect.WithInterceptors(validateInterceptor)))

	api.Handle(blosconnect.NewBLOSServiceHandler(&handlers.BLOSService{
		Cfg:         cfg.Cfg,
		Log:         cfg.Log,
		BLOSManager: cfg.BLOSManager,
	}, connect.WithInterceptors(validateInterceptor)))

	api.Handle(niconnect.NewNetworkInterfaceServiceHandler(&handlers.NetworkInterfaceService{
		Log:        cfg.Log,
		Interfaces: cfg.Interfaces,
		DHCP:       cfg.DHCP,
		Leases:     cfg.Leases,
	}, connect.WithInterceptors(validateInterceptor)))

	api.Handle(dashboardconnect.NewDashboardServiceHandler(&handlers.DashboardService{
		Log:         cfg.Log,
		Board:       &handlers.DefaultBoardProvider{},
		SysInfo:     &system.LinuxSysInfo{},
		Firmware:    &system.OpenWrtFirmwareProvider{},
		Interfaces:  cfg.Interfaces,
		Wifi:        &handlers.DefaultWifiStationProvider{Wifi: cfg.Wifi},
		Originators: &batmanadv.BatctlOriginatorProvider{},
		Tailscale:   cfg.Tailscale,
		Services:    &system.InitDServiceChecker{},
		Actions:     &system.InitDActionExecutor{},
	}, connect.WithInterceptors(validateInterceptor)))

	api.Handle(wificonfigconnect.NewWifiConfigServiceHandler(&handlers.WifiConfigService{
		Log:            cfg.Log,
		IwinfoClient:   iwinfo.NewClient(),
		Wifi:           cfg.Wifi,
		WirelessStatus: network.NewDefaultWirelessStatusProvider(),
		ConfigReader:   network.NewUCIWirelessConfigReader(),
		DHCPLeases:     cfg.Leases,
	}, connect.WithInterceptors(validateInterceptor)))

	// Register auth endpoints when authentication is enabled.
	if cfg.Authenticator != nil && cfg.SessionStore != nil {
		authHandler := &auth.AuthHandler{
			Log:           cfg.Log.With().Str("service", "auth").Logger(),
			Authenticator: cfg.Authenticator,
			Store:         cfg.SessionStore,
		}
		api.HandleFunc("/auth/login", authHandler.HandleLogin)
		api.HandleFunc("/auth/logout", authHandler.HandleLogout)
		api.HandleFunc("/auth/check", authHandler.HandleCheck)
	}

	authMW := auth.NewAPIAuthMiddleware(cfg.SessionStore, cfg.AuthEnabled)

	p := new(http.Protocols)
	p.SetHTTP1(true)
	// Use h2c so we can serve HTTP/2 without TLS.
	p.SetUnencryptedHTTP2(true)
	server := http.Server{
		Addr:           cfg.Cfg.GetOpenMANETAPIAddress(),
		Handler:        withCORS(authMW(api)),
		Protocols:      p,
		ReadTimeout:    30 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 16, // 64 KB
		// WriteTimeout is intentionally omitted to support long-lived
		// streaming RPCs (e.g. CommsService audio streams).
		ErrorLog: logger.StandardLogger(cfg.Log),
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
		// AllowOriginFunc reflects the request origin rather than using a wildcard
		// so that AllowCredentials: true is compatible with the CORS spec. This is
		// safe for a private-network device where all LAN clients are trusted.
		AllowOriginFunc:  func(_ string) bool { return true },
		AllowedMethods:   connectcors.AllowedMethods(),
		AllowedHeaders:   append(connectcors.AllowedHeaders(), "Access-Control-Request-Private-Network"),
		ExposedHeaders:   connectcors.ExposedHeaders(),
		AllowCredentials: true,
		// Crucial for PNA:
		OptionsPassthrough: false,
	})

	wrapped := c.Handler(handler)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Handle the Vary header for caching safety
		w.Header().Add("Vary", "Access-Control-Request-Private-Network")

		// 2. If it's a PNA preflight request, allow it
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}

		// 3. Allow cross-origin embedding so the frontend (served from a
		//    different port with COEP: require-corp) can fetch from this server.
		w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")

		wrapped.ServeHTTP(w, r)
	})
}
