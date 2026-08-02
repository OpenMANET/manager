package tak

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/openmanet/openmanetd/internal/network"
)

type fakeConfigReader map[string][]string

func (r fakeConfigReader) Get(config, section, option string) ([]string, bool) {
	value, ok := r[config+"."+section+"."+option]

	return value, ok
}

func TestDiscoverCameraStreamWith(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		detect  cameraDetector
		reader  configReader
		lookup  interfaceLookup
		want    *CameraStream
		wantErr bool
	}{
		{
			name:   "uses configured camera interface and MediaMTX port",
			detect: func(context.Context) (bool, error) { return true, nil },
			reader: fakeConfigReader{
				"camera-onvif-server.rpicamera.interface": {"ahwlan"},
				"network.ahwlan.device":                   {"br-ahwlan"},
				"mediamtx.@mediamtx[0].rtsp_address":      {":8554"},
			},
			lookup: func(name string) network.NetworkInterface {
				if name != "br-ahwlan" {
					t.Fatalf("lookup device = %q, want br-ahwlan", name)
				}

				return network.NetworkInterface{IP: []network.IPAddress{{IP: net.ParseIP("10.41.0.1")}}}
			},
			want: &CameraStream{Address: "10.41.0.1", Port: 8554, Path: defaultRTSPPath},
		},
		{
			name:   "no detected camera is not an error",
			detect: func(context.Context) (bool, error) { return false, nil },
			want:   nil,
		},
		{
			name:    "camera detection error",
			detect:  func(context.Context) (bool, error) { return false, errors.New("cam unavailable") },
			wantErr: true,
		},
		{
			name:   "falls back to OpenMANET defaults",
			detect: func(context.Context) (bool, error) { return true, nil },
			lookup: func(name string) network.NetworkInterface {
				if name != defaultCameraInterface {
					t.Fatalf("lookup device = %q, want %q", name, defaultCameraInterface)
				}

				return network.NetworkInterface{IP: []network.IPAddress{{IP: net.ParseIP("10.41.2.3")}}}
			},
			want: &CameraStream{Address: "10.41.2.3", Port: defaultRTSPPort, Path: defaultRTSPPath},
		},
		{
			name:   "does not advertise a loopback-only interface",
			detect: func(context.Context) (bool, error) { return true, nil },
			lookup: func(string) network.NetworkInterface {
				return network.NetworkInterface{IP: []network.IPAddress{{IP: net.ParseIP("127.0.0.1")}}}
			},
			wantErr: true,
		},
		{
			name:   "skips invalid and loopback addresses",
			detect: func(context.Context) (bool, error) { return true, nil },
			lookup: func(string) network.NetworkInterface {
				return network.NetworkInterface{IP: []network.IPAddress{
					{IP: net.ParseIP("not-an-ip")},
					{IP: net.ParseIP("127.0.0.1")},
					{IP: net.ParseIP("10.41.2.4")},
				}}
			},
			want: &CameraStream{Address: "10.41.2.4", Port: defaultRTSPPort, Path: defaultRTSPPath},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stream, err := discoverCameraStreamWith(context.Background(), test.detect, test.reader, test.lookup)
			if test.wantErr {
				if err == nil {
					t.Fatal("discoverCameraStreamWith() error = nil, want error")
				}

				return
			}

			if err != nil {
				t.Fatalf("discoverCameraStreamWith() error = %v", err)
			}

			if test.want == nil {
				if stream != nil {
					t.Fatalf("discoverCameraStreamWith() = %+v, want nil", stream)
				}

				return
			}

			if *stream != *test.want {
				t.Fatalf("discoverCameraStreamWith() = %+v, want %+v", stream, test.want)
			}
		})
	}
}

func TestCameraStreamURL(t *testing.T) {
	t.Parallel()

	stream := CameraStream{Address: "10.41.0.1", Port: 8554, Path: "/rpicamera main"}
	if got, want := stream.URL(), "rtsp://10.41.0.1:8554/rpicamera%20main"; got != want {
		t.Fatalf("CameraStream.URL() = %q, want %q", got, want)
	}
}

func TestParseRTSPPort(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]int{
		":8554":          8554,
		"127.0.0.1:8554": 8554,
		"554":            554,
		"":               defaultRTSPPort,
		"not-a-port":     defaultRTSPPort,
		":70000":         defaultRTSPPort,
	} {
		if got := parseRTSPPort(input); got != want {
			t.Errorf("parseRTSPPort(%q) = %d, want %d", input, got, want)
		}
	}
}
