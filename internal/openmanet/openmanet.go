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
	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/database"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/openmanet/server"
	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/rs/zerolog"
)

func Start() {
	var (
		ctx     = context.Background()
		banner  = figure.NewFigure("OpenMANET", "big", true)
		c       = make(chan os.Signal, 1)
		cfg     = config.New(nil)
		log     = logger.InitLogging(ctx)
		gps     *gpsd.GPSService
		manager *mgmt.ManagementConfig
	)

	banner.Print()

	comms := comms.NewComms(comms.CommsConfig{
		Log:                      logger.GetLogger("comms"),
		Interrupt:                c,
		Enable:                   cfg.GetCommsEnable(),
		Iface:                    cfg.GetMeshNetInterface(),
		Debug:                    cfg.GetCommsDebug(),
		Loopback:                 cfg.GetCommsLoopback(),
		Trace:                    cfg.GetCommsTrace(),
		ControlSource:            cfg.GetCommsControlSource(),
		MicGain:                  cfg.GetCommsMicGain(),
		EnableNanoPTT:            cfg.GetCommsNanoPTTEnable(),
		NanoPTTDevicePath:        cfg.GetCommsNanoPTTDevicePath(),
		NanoPTTDeviceName:        cfg.GetCommsNanoPTTDeviceName(),
		EnableBluetoothPtt:       cfg.GetCommsBluetoothPttEnable(),
		BluetoothAudioDeviceHint: cfg.GetCommsBluetoothPttBluetoothAudioDeviceHint(),
		BluetoothInputDevice:     cfg.GetCommsBluetoothPttBluetoothInputDevice(),
		BluetoothOutputDevice:    cfg.GetCommsBluetoothPttBluetoothOutputDevice(),
		PlaybackDepth:            cfg.GetCommsPlaybackBuffer(),
	})

	go comms.Start()

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
	if cfg.GetAlfredEnable() {
		manager, err = mgmt.NewManager(mgmt.ManagementConfig{
			InteruptChan:               c,
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

	// Start API Server
	apiServer := server.APIServer{
		Cfg: cfg,
		Log: logger.GetLogger("api"),
		DB:  db,
		GPS: gps,
	}

	if manager != nil {
		apiServer.Wifi = manager.WirelessConfig
	}

	api := server.NewAPIServer(apiServer)

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

	_ = api.Stop(ctx)
	_ = database.CloseConnection()

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
