package ptt

import (
	"fmt"
	"net"

	"github.com/gordonklaus/portaudio"
	evdev "github.com/gvalkov/golang-evdev"
	"golang.org/x/net/ipv4"
)

func (ptt *PTTConfig) getDeviceByIndex(index int) *portaudio.DeviceInfo {
	devs, err := portaudio.Devices()
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("portaudio.Devices")
	}

	if ptt.Debug {
		ptt.Log.Debug().Msgf("Discovered %d audio devices:", len(devs))
		for i, d := range devs {
			ptt.Log.Debug().Msgf(" [%d] %s", i, d.Name)
		}
	}

	if len(devs) <= index {
		ptt.Log.Fatal().Msgf("Device index %d not found; only %d devices available", index, len(devs))
	}
	return devs[index]
}

func (ptt *PTTConfig) findPTTDevice() *evdev.InputDevice {
	devs, err := evdev.ListInputDevices(ptt.PttDevice)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("evdev.ListInputDevices")
	}

	// Log all available devices if debug is enabled
	if ptt.Debug && len(devs) > 0 {
		ptt.Log.Debug().Msgf("Available HID devices (%d total):", len(devs))
		for _, d := range devs {
			ptt.Log.Debug().Msgf("  - %s (%s)", d.Name, d.Fn)
		}
	}

	// If device name is empty or "AllInOneCable", try to find AIOC device
	if ptt.PttDeviceName == "" || ptt.PttDeviceName == "AllInOneCable" {
		// Try common AIOC device names
		for _, d := range devs {
			if d.Name == "AIOC AIOC" || d.Name == "All-In-One-Cable" {
				ptt.Log.Info().Msgf("Found AIOC PTT device: %s (%s)", d.Name, d.Fn)
				return d
			}
		}
	}

	// Try exact match first
	for _, d := range devs {
		if d.Name == ptt.PttDeviceName {
			ptt.Log.Info().Msgf("Matched PTT device (exact): %s (%s)", d.Name, d.Fn)
			return d
		}
	}

	// Try partial match (case-insensitive substring search)
	for _, d := range devs {
		if len(ptt.PttDeviceName) > 0 && contains(d.Name, ptt.PttDeviceName) {
			ptt.Log.Info().Msgf("Matched PTT device (partial): %s (%s)", d.Name, d.Fn)
			return d
		}
	}

	ptt.Log.Fatal().Msgf("PTT device %q not found. Run with debug=true to see available devices.", ptt.PttDeviceName)
	return nil
}

// contains performs case-insensitive substring search
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1, c2 := s[i+j], substr[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func (ptt *PTTConfig) logInputDeviceList() {
	devs, err := evdev.ListInputDevices(ptt.PttDevice)
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
		return "", nil, err
	}

	addrs, err := ifi.Addrs()
	if err != nil {
		return "", nil, err
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

	return p.JoinGroup(iface, &net.UDPAddr{IP: group})
}
