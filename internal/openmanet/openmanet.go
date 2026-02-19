package openmanet

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/common-nighthawk/go-figure"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/database"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/openmanet/server"
	"github.com/openmanet/openmanetd/internal/ptt"
	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/rs/zerolog"
)

func Start() {
	var (
		ctx    = context.Background()
		banner = figure.NewFigure("OpenMANET", "big", true)
		c      = make(chan os.Signal, 1)
		cfg    = config.New(nil)
		log    = logger.InitLogging(ctx)
		gps    *gpsd.GPSService
	)

	banner.Print()

	ptt := ptt.NewPTT(ptt.PTTConfig{
		Interupt:      c,
		Log:           logger.GetLogger("ptt"),
		Enable:        cfg.GetPTTEnable(),
		Iface:         cfg.GetMeshNetInterface(),
		McastAddr:     cfg.GetPTTMcastAddr(),
		McastPort:     cfg.GetPTTMcastPort(),
		PttKey:        cfg.GetPTTPttKey(),
		Debug:         cfg.GetPTTDebug(),
		Loopback:      cfg.GetPTTLoopback(),
		PttDevice:     cfg.GetPTTPttDevice(),
		PttDeviceName: cfg.GetPTTPttDeviceName(),
	})

	ptt.Start()

	// Init nl80211 wirelsss client
	wirelessCfg, err := mgmt.NewWirelessConfig()
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize wireless configuration")
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

	if cfg.GetEnableGNSS() {
		// Initialize and start GNSS module
		gps, err = gpsd.NewGPSService(logger.GetLogger("gps"), cfg)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to initialize GPS service")
		}
	}

	// Initialize and start management module
	mgmt := mgmt.NewManager(mgmt.ManagementConfig{
		InteruptChan:               c,
		Log:                        logger.GetLogger("mgmt"),
		GPS:                        gps,
		GatewayMode:                cfg.GetGatewayMode(),
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

	mgmt.Start()

	// Clear the batman-adv hosts file on startup
	// to remove any stale entries
	// Stale entries can cause issues with name resolution for nodes that have changed IPs
	// This can also cause issues with gateway selection if the stale entry is for a gateway node
	err = batmanadv.ClearBatHosts()
	if err != nil {
		log.Error().Err(err).Msg("Error clearing batman-adv hosts file on startup")
	}

	// Start API Server
	api := server.NewAPIServer(server.APIServer{
		Cfg:  cfg,
		Wifi: wirelessCfg,
		Log:  logger.GetLogger("api"),
		DB:   db,
		GPS:  gps,
	})
	log.Info().Msg("OpenMANETd API Server starting on port 8087")

	if cfg.BLOSEnabled() {
		// Initialize BLOS module
		_, err := blos.NewBLOS(cfg, logger.GetLogger("blos"))
		if err != nil {
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
	<-c

	api.Stop(ctx)
	database.CloseConnection()
	if cfg.GetEnableGNSS() {
		gps.Close()
	}

	log.Info().Msg("Exiting OpenMANETd")
	os.Exit(0)
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
