package gpsd

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCameraInterface = "ahwlan"
	defaultCameraRTSPPort  = 554
	defaultCameraRTSPPath  = "rpicamera"
	cameraProbeTimeout     = 5 * time.Second
)

var cameraListPattern = regexp.MustCompile(`(?m)^\s*\d+\s*:`)

type cameraStream struct {
	Address string
	Port    int
	Path    string
	URL     string
}

type commandOutputFunc func(context.Context, string, ...string) ([]byte, error)
type interfaceAddrsFunc func(string) ([]net.Addr, error)

func runCommandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // callers use fixed local command names
}

func discoverCameraStream() (*cameraStream, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cameraProbeTimeout)
	defer cancel()

	return discoverCameraStreamWith(ctx, runCommandOutput, interfaceAddresses)
}

func discoverCameraStreamWith(
	ctx context.Context,
	run commandOutputFunc,
	interfaceAddrs interfaceAddrsFunc,
) (*cameraStream, error) {
	output, err := run(ctx, "cam", "-l")
	if err != nil || !cameraListPattern.Match(output) {
		return nil, nil
	}

	iface := uciValue(ctx, run, "camera-onvif-server.rpicamera.interface", defaultCameraInterface)
	device := uciValue(ctx, run, "network."+iface+".device", iface)
	address := firstIPv4Address(interfaceAddrs, device)
	if address == "" {
		address = strings.TrimSpace(uciValue(ctx, run, "network."+iface+".ipaddr", ""))
		if net.ParseIP(address).To4() == nil {
			return nil, fmt.Errorf("camera detected but interface %q has no IPv4 address", iface)
		}
	}

	port := parseRTSPPort(uciValue(ctx, run, "mediamtx.@mediamtx[0].rtsp_address", ""))
	path := strings.TrimSpace(uciValue(
		ctx,
		run,
		"camera-onvif-server.rpicamera.rtsp_name",
		defaultCameraRTSPPath,
	))
	path = "/" + strings.TrimLeft(path, "/")

	streamURL := (&url.URL{
		Scheme: "rtsp",
		Host:   net.JoinHostPort(address, strconv.Itoa(port)),
		Path:   path,
	}).String()

	return &cameraStream{
		Address: address,
		Port:    port,
		Path:    path,
		URL:     streamURL,
	}, nil
}

func uciValue(ctx context.Context, run commandOutputFunc, key, fallback string) string {
	output, err := run(ctx, "uci", "-q", "get", key)
	if err != nil {
		return fallback
	}

	value := strings.TrimSpace(string(output))
	if value == "" {
		return fallback
	}

	return value
}

func interfaceAddresses(name string) ([]net.Addr, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}

	return iface.Addrs()
}

func firstIPv4Address(addrs interfaceAddrsFunc, iface string) string {
	addresses, err := addrs(iface)
	if err != nil {
		return ""
	}

	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			return ip.String()
		}
	}

	return ""
}

func parseRTSPPort(address string) int {
	address = strings.TrimSpace(address)
	if index := strings.LastIndexByte(address, ':'); index >= 0 {
		address = address[index+1:]
	}

	port, err := strconv.Atoi(address)
	if err != nil || port < 1 || port > 65535 {
		return defaultCameraRTSPPort
	}

	return port
}

type sensorXML struct {
	XMLName   xml.Name `xml:"sensor"`
	Elevation string   `xml:"elevation,attr"`
	VFOV      string   `xml:"vfov,attr"`
	FOV       string   `xml:"fov,attr"`
	Azimuth   string   `xml:"azimuth,attr"`
	Range     string   `xml:"range,attr"`
	HideFOV   string   `xml:"hideFov,attr"`
}

type videoXML struct {
	XMLName    xml.Name           `xml:"__video"`
	UID        string             `xml:"uid,attr"`
	URL        string             `xml:"url,attr"`
	Connection videoConnectionXML `xml:"ConnectionEntry"`
}

type videoConnectionXML struct {
	UID               string `xml:"uid,attr"`
	Alias             string `xml:"alias,attr"`
	Address           string `xml:"address,attr"`
	Port              int    `xml:"port,attr"`
	Path              string `xml:"path,attr"`
	Protocol          string `xml:"protocol,attr"`
	RoverPort         int    `xml:"roverPort,attr"`
	RTSPReliable      int    `xml:"rtspReliable,attr"`
	IgnoreEmbeddedKLV bool   `xml:"ignoreEmbeddedKLV,attr"`
	NetworkTimeout    int    `xml:"networkTimeout,attr"`
	BufferTime        int    `xml:"bufferTime,attr"`
}

func cameraCoTXMLDetail(stream *cameraStream, uid, callsign string) (string, error) {
	sensorData, err := xml.Marshal(sensorXML{
		Elevation: "0",
		VFOV:      "45",
		FOV:       "45",
		Azimuth:   "0",
		Range:     "100",
		HideFOV:   "true",
	})
	if err != nil {
		return "", fmt.Errorf("marshal camera sensor detail: %w", err)
	}

	videoData, err := xml.Marshal(videoXML{
		UID: uid,
		URL: stream.URL,
		Connection: videoConnectionXML{
			UID:               uid,
			Alias:             callsign,
			Address:           stream.Address,
			Port:              stream.Port,
			Path:              stream.Path,
			Protocol:          "rtsp",
			RoverPort:         -1,
			RTSPReliable:      0,
			IgnoreEmbeddedKLV: false,
			NetworkTimeout:    12000,
			BufferTime:        -1,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal camera video detail: %w", err)
	}

	return string(sensorData) + string(videoData), nil
}
