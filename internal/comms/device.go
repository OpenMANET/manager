package comms

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/gordonklaus/portaudio"
	evdev "github.com/gvalkov/golang-evdev"
	"golang.org/x/net/ipv4"
)

const (
	defaultControlSourceCM108   = "cm108"
	defaultControlSourceNanoPTT = "nanoptt"
	controlSourceROIP           = "roip"
)

// normalizeControlSource maps raw config strings to canonical control source names.
// Unrecognized values (including empty string) default to "cm108".
func normalizeControlSource(src string) string {
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "bluealsa_xevent":
		return "bluealsa_xevent"
	case defaultControlSourceNanoPTT:
		return defaultControlSourceNanoPTT
	case controlSourceROIP:
		return controlSourceROIP
	default:
		return defaultControlSourceCM108
	}
}

// resolveAudioDevice maps a device spec string to a PortAudio DeviceInfo.
//
//   - ""           → default input or output device
//   - numeric      → index into portaudio.Devices() slice
//   - other string → exact name match, then case-insensitive substring match
func resolveAudioDevice(spec string, wantInput bool) (*portaudio.DeviceInfo, error) {
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

	// Numeric index?
	if idx, err := strconv.Atoi(spec); err == nil {
		if idx < 0 || idx >= len(devices) {
			return nil, fmt.Errorf("audio device index %d out of range (0..%d)", idx, len(devices)-1)
		}

		return devices[idx], nil
	}

	// Exact name match.
	for _, d := range devices {
		if d.Name == spec {
			return d, nil
		}
	}

	// Case-insensitive substring match.
	lowerSpec := strings.ToLower(spec)
	for _, d := range devices {
		if strings.Contains(strings.ToLower(d.Name), lowerSpec) {
			return d, nil
		}
	}

	return nil, fmt.Errorf("audio device %q not found", spec)
}

// findCommDevice searches for a Linux input device whose name matches
// cfg.NanoPTTDeviceName within the glob pattern cfg.NanoPTTDevicePath.
func (cfg *CommsConfig) findCommDevice() *evdev.InputDevice {
	devs, err := evdev.ListInputDevices(cfg.NanoPTTDevicePath)
	if err != nil {
		return nil
	}

	for _, d := range devs {
		if d.Name == cfg.NanoPTTDeviceName {
			return d
		}
	}

	return nil
}

// logInputDeviceList logs all PortAudio devices at Debug level.
func (cfg *CommsConfig) logInputDeviceList() {
	devices, err := portaudio.Devices()
	if err != nil {
		cfg.Log.Debug().Err(err).Msg("Could not list audio devices")

		return
	}

	cfg.Log.Debug().Msgf("Available audio devices (%d):", len(devices))

	for i, d := range devices {
		cfg.Log.Debug().Msgf("  [%d] %s (in=%d out=%d)", i, d.Name, d.MaxInputChannels, d.MaxOutputChannels)
	}
}

// getIfaceIPv4 returns the first IPv4 address on the named network interface
// together with the *net.Interface value (needed for multicast group join).
func getIfaceIPv4(name string) (string, *net.Interface, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return "", nil, fmt.Errorf("interface %q not found: %w", name, err)
	}

	addrs, err := ifi.Addrs()
	if err != nil {
		return "", nil, fmt.Errorf("interface %q addrs: %w", name, err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		if ip4 := ipNet.IP.To4(); ip4 != nil {
			return ip4.String(), ifi, nil
		}
	}

	return "", nil, fmt.Errorf("interface %q has no IPv4 address", name)
}

// joinMulticastGroup joins a UDP connection to an IPv4 multicast group.
func joinMulticastGroup(ifi *net.Interface, conn *net.UDPConn, group net.IP) error {
	pc := ipv4.NewPacketConn(conn)
	if err := pc.JoinGroup(ifi, &net.UDPAddr{IP: group}); err != nil {
		return fmt.Errorf("join multicast group %s on %s: %w", group, ifi.Name, err)
	}

	return nil
}
