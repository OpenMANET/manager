package openmanet

import (
	"context"
	"io/fs"
	"net/http"
	_ "net/http/pprof" //nolint:gosec
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/common-nighthawk/go-figure"
	"github.com/openmanet/openmanetd/internal/auth"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/database"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/frontend"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/instrumentation"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/openmanet/server"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/openmanet/openmanetd/internal/util/board"
	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/rs/zerolog"
)

func Start(staticFS fs.FS) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		banner      = figure.NewFigure("OpenMANET", "big", true)
		c           = make(chan os.Signal, 1)
		cfg         = config.New(nil)
		log         = logger.InitLogging(ctx)
		gps         *gpsd.GPSService
		manager     *mgmt.ManagementConfig
	)

	banner.Print()
	applyRuntimeTuning(cfg, log)

	// Create Comms manager (always, so the API handler can use it even if comms is currently disabled)
	commsManager := comms.NewCommsManager(cfg, logger.GetLogger("comms"))

	if board.CommsSupported() && cfg.GetCommsEnable() {
		if err := commsManager.Enable(); err != nil {
			log.Error().Err(err).Msg("Failed to enable comms module")
		}
	} else if !board.CommsSupported() {
		log.Warn().Msg("Current board does not support Comms features; skipping initialization of comms module")
	}

	// Establish database connection
	db, err := database.NewConnection(ctx, logger.GetLogger("database"), cfg.GetDBFile())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}

	if cfg.GetResetDBOnStart() {
		err = resetDBOnStart(ctx, db, logger.GetLogger("database"))
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to reset database on start")
		}
	}

	if cfg.GetEnableGNSS() && board.GNSSsupoorted() {
		// Initialize and start GNSS module
		gps, err = gpsd.NewGPSService(logger.GetLogger("gps"), cfg)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to initialize GPS service")
		}
	}

	// Initialize and start management module
	if cfg.GetAlfredEnable() {
		manager, err = mgmt.NewManager(mgmt.ManagementConfig{
			Log:                        logger.GetLogger("mgmt"),
			GPS:                        gps,
			AlfredMode:                 cfg.GetAlfredMode(),
			IFace:                      cfg.GetMeshNetInterface(),
			BatInterface:               cfg.GetAlfredBatInterface(),
			SocketPath:                 cfg.GetAlfredSocketPath(),
			GatewayDataType:            cfg.GetAlfredDataTypeGateway(),
			NodeDataType:               cfg.GetAlfredDataTypeNode(),
			PositionDataType:           cfg.GetAlfredDataTypePosition(),
			AddressReservationDataType: cfg.GetAlfredDataTypeAddressReservation(),
			BatmanMulticastForceflood:  cfg.GetBatmanMulticastForceflood(),
			DB:                         db,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to initialize management workers")
		}

		manager.Start(ctx)
	} else {
		log.Info().Msg("Alfred integration disabled; skipping management workers")
	}

	// Clear the batman-adv hosts file on startup
	// to remove any stale entries
	// Stale entries can cause issues with name resolution for nodes that have changed IPs
	// This can also cause issues with gateway selection if the stale entry is for a gateway node
	err = batmanadv.ClearBatHosts()
	if err != nil {
		log.Error().Err(err).Msg("Error clearing batman-adv hosts file on startup")
	}

	// Create BLOS manager (always, so the API handler can use it even if BLOS is currently disabled)
	blosManager := blos.NewBLOSManager(cfg, logger.GetLogger("blos"))

	// Wire the instrumentation snapshot registry and conditionally spawn
	// the periodic worker. The registry is always constructed (cheap) but
	// the worker goroutine is only started when the config flag is true,
	// so a disabled deployment pays nothing beyond the adapter structs.
	startInstrumentationWorker(ctx, cfg, blosManager, log)

	// Build the originator providers. The raw provider shells out to batctl
	// and is shared with the delta tracker, which only needs the edge
	// tuples. The enriched provider layers bat-hosts + self-MAC + hop
	// derivation on top for the RPC handler. Sharing the raw provider
	// halves the number of `batctl oj` shell invocations in the hot path.
	rawOrigProvider := &batmanadv.BatctlOriginatorProvider{}
	meshOrigProvider := &batmanadv.BatctlOriginatorTopologyProvider{
		Originators: rawOrigProvider,
	}

	// Start the mesh-topology delta tracker. The tracker polls the
	// originator table on a fixed cadence and keeps a rolling snapshot
	// ring so the MeshTopologyService.GetMeshTopologyDelta RPC can return
	// churn metrics without re-shelling out per call. Exits on ctx
	// cancellation.
	meshDeltaTracker := handlers.NewDeltaTracker(
		logger.GetLogger("mesh-delta"),
		rawOrigProvider,
		handlers.BatctlGatewayProvider{},
		time.Duration(cfg.GetMeshTopologyDeltaSampleInterval())*time.Second,
		cfg.GetMeshTopologyMaxDeltaSamples(),
	)
	meshDeltaTracker.Start(ctx)

	// Set up session-based authentication when enabled.
	var (
		sessionStore  *auth.SessionStore
		authenticator auth.Authenticator
	)

	if cfg.GetAuthEnable() {
		sessionStore = auth.NewSessionStore(
			time.Duration(cfg.GetAuthSessionMaxAgeSecs())*time.Second,
			cfg.GetAuthSessionMaxSize(),
		)
		sessionStore.StartCleanup(ctx, 5*time.Minute)

		authenticator = &auth.PAMAuthenticator{ServiceName: cfg.GetAuthPAMService()}
		log.Info().Str("pamService", cfg.GetAuthPAMService()).Msg("authentication enabled")
	}

	// Start API Server
	interfaceProvider := &network.NetlinkInterfaceProvider{}

	apiServer := server.APIServer{
		Cfg:              cfg,
		Log:              logger.GetLogger("api"),
		DB:               db,
		GPS:              gps,
		BLOSManager:      blosManager,
		Tailscale:        blosManager,
		CommsManager:     commsManager,
		MeshDeltaTracker: meshDeltaTracker,
		MeshOrigProvider: meshOrigProvider,
		Interfaces:       interfaceProvider,
		DHCP: &network.UCIDHCPConfigProvider{
			DHCPReader:    network.NewUCIDHCPConfigReader(),
			NetworkReader: network.NewUCINetworkConfigReader(),
		},
		Leases: &network.UbusLeaseProvider{
			Executor: &network.DefaultUbusExecutor{},
		},
		SessionStore:  sessionStore,
		Authenticator: authenticator,
		AuthEnabled:   cfg.GetAuthEnable(),
	}

	if manager != nil {
		apiServer.Wifi = manager.WirelessConfig
		interfaceProvider.WifiInterfaces = manager.WirelessConfig.Interfaces
	}

	api := server.NewAPIServer(apiServer)

	log.Info().Msg("OpenMANETd API Server starting on port 8087")

	if cfg.BLOSEnabled() && board.BLOSsupported() {
		if err := blosManager.Enable(context.Background()); err != nil {
			log.Fatal().Err(err).Msg("Failed to initialize BLOS module")
		}
	}

	// Wait for interrupt signal to gracefully shutdown the application
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Start the API server in a goroutine so it doesn't block
	go func() {
		if err := api.ApiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("API Server failed")
		}
	}()

	frontendServer := frontend.NewFrontendServer(ctx, cfg, staticFS, sessionStore, cfg.GetAuthEnable())

	go func() {
		if err := frontendServer.Run(ctx); err != nil {
			log.Error().Err(err).Msg("Frontend Server failed")
		}
	}()

	// Block until we receive an interrupt signal, then gracefully shutdown.
	<-c

	// Cancel context to signal all context-aware goroutines (mgmt workers, hub, etc.)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)

	_ = api.Stop(shutdownCtx)

	shutdownCancel()

	_ = database.CloseConnection()

	commsManager.Disable()
	blosManager.Disable()

	if cfg.GetEnableGNSS() {
		gps.Close()
	}

	log.Info().Msg("Exiting OpenMANETd")
	os.Exit(0)
}

// startInstrumentationWorker constructs the instrumentation snapshot
// registry, registers the comms and BLOS adapters, and starts the
// periodic worker goroutine when the config flag is enabled. The
// registry itself is cheap; only the worker has runtime cost. Errors
// during setup are logged but never fatal — a misconfigured snapshot
// subsystem must not prevent the daemon from serving traffic.
func startInstrumentationWorker(ctx context.Context, cfg *config.Config, blosManager *blos.BLOSManager, log zerolog.Logger) {
	if !cfg.GetInstrumentationEnable() {
		return
	}

	instrLog := logger.GetLogger("instrumentation")

	hostname, err := os.Hostname()
	if err != nil {
		instrLog.Warn().Err(err).Msg("instrumentation: failed to read hostname; leaving empty")

		hostname = ""
	}

	reg := instrumentation.NewRegistry(instrumentation.Options{
		Log:      instrLog,
		Version:  "", // populated from build metadata when available
		Hostname: hostname,
	})

	if err = reg.Register("comms", &comms.CommsSnapshotter{}); err != nil {
		log.Error().Err(err).Msg("instrumentation: failed to register comms snapshotter")

		return
	}

	if err = reg.Register("blos", &blos.BLOSSnapshotter{Manager: blosManager}); err != nil {
		log.Error().Err(err).Msg("instrumentation: failed to register blos snapshotter")

		return
	}

	worker, err := instrumentation.NewWorker(instrumentation.WorkerOptions{
		Registry:       reg,
		Interval:       time.Duration(cfg.GetInstrumentationIntervalSecs()) * time.Second,
		OutputDir:      cfg.GetInstrumentationSnapshotDir(),
		FilenamePrefix: "openmanetd-snapshot",
		Log:            instrLog,
	})
	if err != nil {
		log.Error().Err(err).Msg("instrumentation: failed to construct snapshot worker")

		return
	}

	go worker.Run(ctx)
}

// applyRuntimeTuning configures Go runtime parameters and optionally starts
// the pprof debug endpoint based on the application configuration.
func applyRuntimeTuning(cfg *config.Config, log zerolog.Logger) {
	if cfg.GetDebugPprof() {
		pprofAddr := cfg.GetDebugPprofAddress()

		go func() {
			log.Info().Str("addr", pprofAddr).Msg("pprof debug endpoint enabled")

			if err := http.ListenAndServe(pprofAddr, nil); err != nil { //nolint:gosec
				log.Error().Err(err).Msg("pprof server failed")
			}
		}()
	}
}

func resetDBOnStart(ctx context.Context, db *models.Queries, log zerolog.Logger) error {
	log.Info().Msg("Resetting database on start as per configuration")
	// Add any additional tables that need to be cleared here
	err := db.DeleteAllMeshNodes(ctx)
	if err != nil {
		return err
	}

	return nil
}
