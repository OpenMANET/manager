package ptt

import (
	"fmt"
	"log"
	"net"

	"github.com/gordonklaus/portaudio"
	evdev "github.com/gvalkov/golang-evdev"
	"golang.org/x/net/ipv4"
)

func (ptt *PTTConfig) getDeviceByIndex(index int) *portaudio.DeviceInfo {
	devs, err := portaudio.Devices()
	if err != nil {
		log.Fatalf("portaudio.Devices: %v", err)
	}

	if len(devs) <= index {
		log.Fatalf("Device index %d not found; only %d devices available", index, len(devs))
	}
	return devs[index]
}

func (ptt *PTTConfig) findPTTDevice() *evdev.InputDevice {
	devs, err := evdev.ListInputDevices()
	if err != nil {
		log.Fatalf("evdev.ListInputDevices: %v", err)
	}

	for _, d := range devs {
		if d.Name == ptt.PttDevice {
			ptt.Log.Debug().Msgf("Matched PTT device %s (%s)", d.Name, d.Fn)

			return d
		}
	}
	ptt.Log.Fatal().Msgf("PTT device %q not found", ptt.PttDevice)

	return nil
}

func (ptt *PTTConfig) logInputDeviceList() {
	devs, err := evdev.ListInputDevices()
	if err != nil {
		ptt.Log.Error().Err(err).Msg("Unable to list input devices")
		return
	}

	ptt.Log.Info().Msgf("Discovered %d input devices:", len(devs))
	for _, d := range devs {
		ptt.Log.Info().Msgf(" - %s (%s)", d.Name, d.Fn)
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
