//go:build !omd_omit_comms

package openmanet

import (
	"os"

	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/util/logger"
)

// startComms initializes and starts the communications subsystem with the provided configuration.
// It creates a new Comms instance using settings derived from the given config.Config, including
// network interface settings, PTT (Push-To-Talk) parameters, audio device configuration,
// and multicast address/port settings. The interrupt channel is used to signal graceful shutdown.
// The communications subsystem is started asynchronously in a separate goroutine.
//
// Parameters:
//   - cfg: A pointer to a config.Config instance containing all necessary configuration values
//     for the communications subsystem, including network, audio, and PTT settings.
//   - interrupt: A channel used to receive os.Signal notifications for graceful shutdown handling.
func startComms(cfg *config.Config, interrupt chan os.Signal) {
	c := comms.NewComms(comms.CommsConfig{
		Log:             logger.GetLogger("comms"),
		Interrupt:       interrupt,
		Enable:          cfg.GetPTTEnable(),
		Iface:           cfg.GetMeshNetInterface(),
		McastAddr:       cfg.GetPTTMcastAddr(),
		McastPort:       cfg.GetPTTMcastPort(),
		RtpID:           cfg.GetPTTRtpID(),
		CommKey:         cfg.GetPTTPttKey(),
		Debug:           cfg.GetPTTDebug(),
		Loopback:        cfg.GetPTTLoopback(),
		Trace:           cfg.GetPTTTrace(),
		CommDeviceGlob:  cfg.GetPTTPttDevice(),
		CommDeviceName:  cfg.GetPTTPttDeviceName(),
		ControlSource:   cfg.GetPTTControlSource(),
		AudioDeviceHint: cfg.GetPTTAudioDeviceHint(),
		InputDevice:     cfg.GetPTTInputDevice(),
		OutputDevice:    cfg.GetPTTOutputDevice(),
		PlaybackDepth:   cfg.GetPTTPlaybackBuffer(),
		MicGain:         cfg.GetPTTMicGain(),
	})

	go c.Start()
}
