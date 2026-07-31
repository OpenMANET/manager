// Package tak builds Cursor-on-Target messages that describe OpenMANET nodes
// and their TAK-visible capabilities.
package tak

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openmanet/openmanetd/internal/network"
)

const (
	defaultCameraInterface = "ahwlan"
	defaultRTSPPort        = 554
	defaultRTSPPath        = "/rpicamera"
	cameraProbeTimeout     = 2 * time.Second
)

var cameraListPattern = regexp.MustCompile(`(?m)^\s*\d+\s*:`) //nolint:gochecknoglobals // compiled once for camera probe output

// CameraStream is the RTSP endpoint that ATAK should use for a node camera.
type CameraStream struct {
	Address string
	Path    string
	Port    int
}

// URL returns the canonical RTSP URL for the stream.
func (s CameraStream) URL() string {
	return (&url.URL{
		Scheme: "rtsp",
		Host:   net.JoinHostPort(s.Address, strconv.Itoa(s.Port)),
		Path:   s.Path,
	}).String()
}

type configReader interface {
	Get(config, section, option string) ([]string, bool)
}

type cameraDetector func(context.Context) (bool, error)
type interfaceLookup func(string) network.NetworkInterface

// DiscoverCameraStream returns the supported camera RTSP endpoint when a
// libcamera-compatible sensor is present and its configured interface has a
// live IPv4 address. It intentionally advertises only the OpenMANET stream
// contract: MediaMTX's configured port and the rpicamera path.
func DiscoverCameraStream(ctx context.Context) (*CameraStream, error) {
	ctx, cancel := context.WithTimeout(ctx, cameraProbeTimeout)
	defer cancel()

	return discoverCameraStreamWith(ctx, detectCamera, network.NewUCINetworkConfigReader(), network.GetInterfaceByName)
}

func discoverCameraStreamWith(
	ctx context.Context,
	detect cameraDetector,
	reader configReader,
	lookup interfaceLookup,
) (*CameraStream, error) {
	present, err := detect(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect camera: %w", err)
	}
	if !present {
		return nil, nil
	}

	iface := configValue(reader, "camera-onvif-server", "rpicamera", "interface", defaultCameraInterface)
	device := configValue(reader, "network", iface, "device", iface)
	address := firstIPv4Address(lookup(device))
	if address == "" {
		return nil, fmt.Errorf("camera interface %q (%s) has no live IPv4 address", iface, device)
	}

	return &CameraStream{
		Address: address,
		Port:    parseRTSPPort(configValue(reader, "mediamtx", "@mediamtx[0]", "rtsp_address", "")),
		Path:    defaultRTSPPath,
	}, nil
}

func detectCamera(ctx context.Context) (bool, error) {
	output, err := exec.CommandContext(ctx, "cam", "-l").Output() //nolint:gosec // fixed local executable and arguments
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, nil
		}

		return false, fmt.Errorf("run cam -l: %w", err)
	}

	return cameraListPattern.Match(output), nil
}

func configValue(reader configReader, config, section, option, fallback string) string {
	if reader == nil {
		return fallback
	}

	values, ok := reader.Get(config, section, option)
	if !ok || len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return fallback
	}

	return strings.TrimSpace(values[0])
}

func firstIPv4Address(iface network.NetworkInterface) string {
	for _, address := range iface.IP {
		ip := address.IP.To4()
		if ip != nil && !ip.IsLoopback() {
			return ip.String()
		}
	}

	return ""
}

func parseRTSPPort(address string) int {
	address = strings.TrimSpace(address)
	if _, port, err := net.SplitHostPort(address); err == nil {
		return validPort(port)
	}

	if strings.HasPrefix(address, ":") {
		return validPort(strings.TrimPrefix(address, ":"))
	}

	return validPort(address)
}

func validPort(value string) int {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return defaultRTSPPort
	}

	return port
}
