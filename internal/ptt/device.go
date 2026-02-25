package ptt

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/gordonklaus/portaudio"
	evdev "github.com/gvalkov/golang-evdev"
	"golang.org/x/net/ipv4"
)

func (ptt *PTTConfig) resolveAudioDevice(spec string, wantInput bool) (*portaudio.DeviceInfo, error) {
	devs, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("list audio devices: %w", err)
	}

	if ptt.Debug {
		ptt.Log.Debug().Msgf("Discovered %d audio devices:", len(devs))

		for i, d := range devs {
			ptt.Log.Debug().Msgf(" [%d] %s (in=%d out=%d)", i, d.Name, d.MaxInputChannels, d.MaxOutputChannels)
		}
	}

	if spec == "" {
		if wantInput {
			dev, err := portaudio.DefaultInputDevice()
			if err != nil {
				return nil, fmt.Errorf("get default input device: %w", err)
			}

			return dev, nil
		}

		dev, err := portaudio.DefaultOutputDevice()
		if err != nil {
			return nil, fmt.Errorf("get default output device: %w", err)
		}

		return dev, nil
	}

	if idx, err := strconv.Atoi(spec); err == nil {
		if idx < 0 || idx >= len(devs) {
			return nil, fmt.Errorf("audio device index %d out of range (0-%d)", idx, len(devs)-1)
		}

		return devs[idx], nil
	}

	for _, d := range devs {
		if wantInput && d.MaxInputChannels == 0 {
			continue
		}

		if !wantInput && d.MaxOutputChannels == 0 {
			continue
		}

		if d.Name == spec {
			return d, nil
		}
	}

	specLower := strings.ToLower(spec)

	for _, d := range devs {
		if wantInput && d.MaxInputChannels == 0 {
			continue
		}

		if !wantInput && d.MaxOutputChannels == 0 {
			continue
		}

		if strings.Contains(strings.ToLower(d.Name), specLower) {
			return d, nil
		}
	}

	return nil, fmt.Errorf("audio device %q not found", spec)
}

func normalizeControlSource(src string) string {
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "bluealsa_xevent":
		return "bluealsa_xevent"
	default:
		return "evdev"
	}
}

// findPTTDevice searches for the configured evdev device and returns it, or
// nil if it cannot be found (error is logged).  Callers should treat a nil
// return as a configuration error.
func (ptt *PTTConfig) findPTTDevice() *evdev.InputDevice {
	devs, err := evdev.ListInputDevices(ptt.PTTDeviceGlob)
	if err != nil {
		ptt.Log.Error().Err(err).Msg("evdev.ListInputDevices")

		return nil
	}

	for _, d := range devs {
		if d.Name == ptt.PTTDeviceName {
			ptt.Log.Debug().Msgf("Matched PTT device %s (%s)", d.Name, d.Fn)

			return d
		}
	}

	ptt.Log.Error().Msgf("PTT device %q not found", ptt.PTTDeviceName)

	return nil
}

func (ptt *PTTConfig) logInputDeviceList() {
	devs, err := evdev.ListInputDevices(ptt.PTTDeviceGlob)
	if err != nil {
		ptt.Log.Error().Err(err).Msg("Unable to list input devices")

		return
	}

	ptt.Log.Debug().Msgf("Discovered %d input devices:", len(devs))

	for _, d := range devs {
		ptt.Log.Debug().Interface("input-device", d).Msgf(" - %s (%s)", d.Name, d.Fn)
	}
}

func (ptt *PTTConfig) getIfaceIPv4(name string) (string, *net.Interface, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return "", nil, fmt.Errorf("get interface %s: %w", name, err)
	}

	addrs, err := ifi.Addrs()
	if err != nil {
		return "", nil, fmt.Errorf("get addresses for %s: %w", name, err)
	}

	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.String(), ifi, nil
		}
	}

	return "", ifi, fmt.Errorf("no IPv4 on iface %s", name)
}

func (ptt *PTTConfig) joinMulticastGroup(iface *net.Interface, conn *net.UDPConn, group net.IP) error {
	p := ipv4.NewPacketConn(conn)

	if err := p.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
		return fmt.Errorf("join multicast group %s on %s: %w", group, iface.Name, err)
	}

	return nil
}
