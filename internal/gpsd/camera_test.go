package gpsd

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestDiscoverCameraStream(t *testing.T) {
	responses := map[string]string{
		"camera-onvif-server.rpicamera.interface": "ahwlan\n",
		"network.ahwlan.device":                   "br-ahwlan\n",
		"mediamtx.@mediamtx[0].rtsp_address":      ":8554\n",
		"camera-onvif-server.rpicamera.rtsp_name": "camera/main\n",
	}

	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "cam" {
			return []byte("Available cameras:\n1: 'imx219'\n"), nil
		}

		if name != "uci" || len(args) != 3 {
			return nil, errors.New("unexpected command")
		}

		value, ok := responses[args[2]]
		if !ok {
			return nil, errors.New("missing UCI value")
		}

		return []byte(value), nil
	}
	addrs := func(name string) ([]net.Addr, error) {
		if name != "br-ahwlan" {
			t.Fatalf("interface = %q, want br-ahwlan", name)
		}

		return []net.Addr{&net.IPNet{IP: net.ParseIP("10.41.0.1"), Mask: net.CIDRMask(16, 32)}}, nil
	}

	stream, err := discoverCameraStreamWith(context.Background(), run, addrs)
	if err != nil {
		t.Fatalf("discoverCameraStreamWith() error = %v", err)
	}

	if stream == nil {
		t.Fatal("discoverCameraStreamWith() returned no stream")
	}

	if stream.URL != "rtsp://10.41.0.1:8554/camera/main" {
		t.Errorf("URL = %q", stream.URL)
	}

	if stream.Address != "10.41.0.1" || stream.Port != 8554 || stream.Path != "/camera/main" {
		t.Errorf("stream = %+v", stream)
	}
}

func TestDiscoverCameraStreamNoCamera(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("Available cameras:\n"), nil
	}

	stream, err := discoverCameraStreamWith(context.Background(), run, nil)
	if err != nil {
		t.Fatalf("discoverCameraStreamWith() error = %v", err)
	}

	if stream != nil {
		t.Errorf("stream = %+v, want nil", stream)
	}
}

func TestDiscoverCameraStreamFallsBackToFirmwareDefaults(t *testing.T) {
	run := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "cam" {
			return []byte("1: 'imx708'\n"), nil
		}

		return nil, errors.New("UCI unavailable")
	}
	addrs := func(name string) ([]net.Addr, error) {
		if name != defaultCameraInterface {
			t.Fatalf("interface = %q, want %q", name, defaultCameraInterface)
		}

		return []net.Addr{&net.IPNet{IP: net.ParseIP("10.41.2.3"), Mask: net.CIDRMask(16, 32)}}, nil
	}

	stream, err := discoverCameraStreamWith(context.Background(), run, addrs)
	if err != nil {
		t.Fatalf("discoverCameraStreamWith() error = %v", err)
	}

	if stream.URL != "rtsp://10.41.2.3:554/rpicamera" {
		t.Errorf("URL = %q", stream.URL)
	}
}

func TestDiscoverCameraStreamFallsBackToInterfaceIPAddress(t *testing.T) {
	responses := map[string]string{
		"camera-onvif-server.rpicamera.interface": "ahwlan\n",
		"network.ahwlan.device":                   "br-ahwlan\n",
		"network.ahwlan.ipaddr":                   "10.41.9.7\n",
		"mediamtx.@mediamtx[0].rtsp_address":      " \n",
		"camera-onvif-server.rpicamera.rtsp_name": "/nested/path\n",
	}

	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "cam" {
			return []byte("1: 'imx708'\n"), nil
		}

		value, ok := responses[args[2]]
		if !ok {
			return nil, errors.New("missing UCI value")
		}

		return []byte(value), nil
	}
	addrs := func(string) ([]net.Addr, error) {
		return nil, errors.New("no interface addresses")
	}

	stream, err := discoverCameraStreamWith(context.Background(), run, addrs)
	if err != nil {
		t.Fatalf("discoverCameraStreamWith() error = %v", err)
	}

	if stream == nil {
		t.Fatal("discoverCameraStreamWith() returned no stream")
	}

	if stream.Address != "10.41.9.7" || stream.Port != defaultCameraRTSPPort || stream.Path != "/nested/path" {
		t.Errorf("stream = %+v", stream)
	}
}

func TestDiscoverCameraStreamReturnsErrorWithoutIPv4Address(t *testing.T) {
	responses := map[string]string{
		"camera-onvif-server.rpicamera.interface": "ahwlan\n",
		"network.ahwlan.device":                   "br-ahwlan\n",
		"network.ahwlan.ipaddr":                   "not-an-ip\n",
	}

	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "cam" {
			return []byte("1: 'imx708'\n"), nil
		}

		value, ok := responses[args[2]]
		if !ok {
			return nil, errors.New("missing UCI value")
		}

		return []byte(value), nil
	}
	addrs := func(string) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)}}, nil
	}

	stream, err := discoverCameraStreamWith(context.Background(), run, addrs)
	if err == nil || !strings.Contains(err.Error(), `interface "ahwlan" has no IPv4 address`) {
		t.Fatalf("discoverCameraStreamWith() error = %v, want missing IPv4 error", err)
	}

	if stream != nil {
		t.Errorf("stream = %+v, want nil", stream)
	}
}

func TestUCIValueReturnsFallbackForBlankOutput(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(" \n\t"), nil
	}

	if got := uciValue(context.Background(), run, "ignored", "fallback"); got != "fallback" {
		t.Fatalf("uciValue() = %q, want fallback", got)
	}
}

func TestRunCommandOutputReturnsWrappedError(t *testing.T) {
	_, err := runCommandOutput(context.Background(), "sh", "-c", "exit 7")
	if err == nil || !strings.Contains(err.Error(), "run sh") {
		t.Fatalf("runCommandOutput() error = %v, want wrapped command error", err)
	}
}

func TestRunCommandOutputReturnsCommandOutput(t *testing.T) {
	output, err := runCommandOutput(context.Background(), "sh", "-c", "printf camera-ready")
	if err != nil {
		t.Fatalf("runCommandOutput() error = %v", err)
	}

	if string(output) != "camera-ready" {
		t.Fatalf("runCommandOutput() output = %q, want camera-ready", string(output))
	}
}

type stubAddr string

func (a stubAddr) Network() string { return "stub" }

func (a stubAddr) String() string { return string(a) }

func TestFirstIPv4AddressIgnoresLoopbackAndInvalidAddresses(t *testing.T) {
	addrs := func(string) ([]net.Addr, error) {
		return []net.Addr{
			stubAddr("not-a-cidr"),
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
			&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
			&net.IPNet{IP: net.ParseIP("10.41.1.25"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}

	if got := firstIPv4Address(addrs, "br-ahwlan"); got != "10.41.1.25" {
		t.Fatalf("firstIPv4Address() = %q, want 10.41.1.25", got)
	}
}

func TestInterfaceAddressesReturnsLookupError(t *testing.T) {
	_, err := interfaceAddresses("definitely-not-a-real-interface")
	if err == nil || !strings.Contains(err.Error(), "lookup interface") {
		t.Fatalf("interfaceAddresses() error = %v, want lookup error", err)
	}
}

func TestInterfaceAddressesReturnsAddressesForExistingInterface(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces() error = %v", err)
	}

	if len(interfaces) == 0 {
		t.Fatal("net.Interfaces() returned no interfaces")
	}

	addrs, err := interfaceAddresses(interfaces[0].Name)
	if err != nil {
		t.Fatalf("interfaceAddresses() error = %v", err)
	}

	if addrs == nil {
		t.Fatal("interfaceAddresses() returned nil addresses")
	}
}

func TestCameraCoTXMLDetail(t *testing.T) {
	stream := &cameraStream{
		Address: "10.41.0.1",
		Port:    554,
		Path:    "/rpicamera",
		URL:     "rtsp://10.41.0.1:554/rpicamera",
	}

	detail, err := cameraCoTXMLDetail(stream, "node-1", `CAM & MANET`)
	if err != nil {
		t.Fatalf("cameraCoTXMLDetail() error = %v", err)
	}

	checks := []string{
		`<sensor `,
		`hideFov="true"`,
		`<__video uid="node-1" url="rtsp://10.41.0.1:554/rpicamera">`,
		`<ConnectionEntry `,
		`uid="node-1"`,
		`alias="CAM &amp; MANET"`,
		`address="10.41.0.1"`,
		`port="554"`,
		`path="/rpicamera"`,
		`protocol="rtsp"`,
	}
	for _, check := range checks {
		if !strings.Contains(detail, check) {
			t.Errorf("detail does not contain %q: %s", check, detail)
		}
	}
}

func TestParseRTSPPort(t *testing.T) {
	tests := map[string]int{
		":8554":          8554,
		"127.0.0.1:8554": 8554,
		"554":            554,
		"":               554,
		"invalid":        554,
		":70000":         554,
	}

	for input, want := range tests {
		if got := parseRTSPPort(input); got != want {
			t.Errorf("parseRTSPPort(%q) = %d, want %d", input, got, want)
		}
	}
}
