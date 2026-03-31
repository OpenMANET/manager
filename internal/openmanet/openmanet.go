package openmanet

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	_ "net/http/pprof" //nolint:gosec
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/common-nighthawk/go-figure"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/database"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/frontend"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/openmanet/server"
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

	// Start API Server
	interfaceProvider := &network.NetlinkInterfaceProvider{}

	apiServer := server.APIServer{
		Cfg:          cfg,
		Log:          logger.GetLogger("api"),
		DB:           db,
		GPS:          gps,
		BLOSManager:  blosManager,
		CommsManager: commsManager,
		Interfaces:   interfaceProvider,
		DHCP: &network.UCIDHCPConfigProvider{
			DHCPReader:    network.NewUCIDHCPConfigReader(),
			NetworkReader: network.NewUCINetworkConfigReader(),
		},
		Leases: &network.UbusLeaseProvider{
			Executor: &network.DefaultUbusExecutor{},
		},
	}

	if manager != nil {
		apiServer.Wifi = manager.WirelessConfig
		interfaceProvider.WifiInterfaces = manager.WirelessConfig.Interfaces
	}

	api := server.NewAPIServer(apiServer)

	log.Info().Msg("OpenMANETd API Server starting on port 8087")

	if cfg.BLOSEnabled() && board.BLOSsupported() {
		if err := blosManager.Enable(); err != nil {
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

	frontendServer := frontend.NewFrontendServer(ctx, cfg, staticFS)

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

// applyRuntimeTuning configures Go runtime parameters and optionally starts
// the pprof debug endpoint based on the application configuration.
func applyRuntimeTuning(cfg *config.Config, log zerolog.Logger) {
	debug.SetGCPercent(cfg.GetRuntimeGoGC())

	if limit, err := parseMemLimit(cfg.GetRuntimeMemLimit()); err == nil {
		debug.SetMemoryLimit(limit)
		log.Info().Int64("bytes", limit).Msg("runtime memory limit set")
	} else {
		log.Warn().Err(err).Msg("invalid runtime.memlimit value; using Go default")
	}

	log.Info().
		Int("GOGC", cfg.GetRuntimeGoGC()).
		Msg("runtime tuning applied")

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

// parseMemLimit converts a human-readable memory string (e.g. "64MiB", "256MiB",
// "1GiB") into bytes. Supported suffixes: KiB, MiB, GiB.
func parseMemLimit(s string) (int64, error) {
	s = strings.TrimSpace(s)

	type suffix struct {
		name string
		mult int64
	}

	for _, sf := range []suffix{
		{"GiB", 1 << 30},
		{"MiB", 1 << 20},
		{"KiB", 1 << 10},
	} {
		if strings.HasSuffix(s, sf.name) {
			num := strings.TrimSuffix(s, sf.name)

			v, err := strconv.ParseInt(strings.TrimSpace(num), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse %q: %w", s, err)
			}

			return v * sf.mult, nil
		}
	}

	// Plain integer treated as bytes.
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", s, err)
	}

	return v, nil
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
