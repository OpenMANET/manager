package device

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"
)

// ResolveAudio maps a device spec string to a PortAudio DeviceInfo.
//
//   - ""           → default input or output device
//   - numeric      → index into portaudio.Devices() slice
//   - other string → exact name match, then case-insensitive substring match
func ResolveAudio(spec string, wantInput bool) (*portaudio.DeviceInfo, error) {
	if spec == "" {
		if wantInput {
			dev, err := portaudio.DefaultInputDevice()
			if err != nil {
				return nil, fmt.Errorf("portaudio.DefaultInputDevice: %w", err)
			}

			return dev, nil
		}

		dev, err := portaudio.DefaultOutputDevice()
		if err != nil {
			return nil, fmt.Errorf("portaudio.DefaultOutputDevice: %w", err)
		}

		return dev, nil
	}

	devices, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("portaudio.Devices: %w", err)
	}

	if idx, err := strconv.Atoi(spec); err == nil {
		if idx < 0 || idx >= len(devices) {
			return nil, fmt.Errorf("audio device index %d out of range (0..%d)", idx, len(devices)-1)
		}

		return devices[idx], nil
	}

	for _, d := range devices {
		if d.Name == spec {
			return d, nil
		}
	}

	lowerSpec := strings.ToLower(spec)
	for _, d := range devices {
		if strings.Contains(strings.ToLower(d.Name), lowerSpec) {
			return d, nil
		}
	}

	return nil, fmt.Errorf("audio device %q not found", spec)
}

// LogPortAudioDevices logs all PortAudio devices at Debug level on log.
func LogPortAudioDevices(log zerolog.Logger) {
	devices, err := portaudio.Devices()
	if err != nil {
		log.Debug().Err(err).Msg("Could not list audio devices")

		return
	}

	log.Debug().Msgf("Available audio devices (%d):", len(devices))

	for i, d := range devices {
		log.Debug().Msgf("  [%d] %s (in=%d out=%d)", i, d.Name, d.MaxInputChannels, d.MaxOutputChannels)
	}
}
