package camera

import (
	"context"
	"net"
	"testing"

	"github.com/openmanet/openmanetd/internal/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect_missingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	present, err := Detect(context.Background())
	require.NoError(t, err)
	assert.False(t, present)
}

func TestCameraListContainsSensor_output(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "camera", output: "Available cameras:\n------------------\n0 : imx219 [3280x2464]", want: true},
		{name: "no camera", output: "Available cameras:\n------------------\n", want: false},
		{name: "unrelated colon", output: "Available cameras:\nnone: detected", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, cameraListContainsSensor([]byte(tc.output)))
		})
	}
}

func TestResolveStream_configuredEndpoint(t *testing.T) {
	t.Parallel()

	reader := newFakeConfigReader(t, map[string][]string{
		"camera-onvif-server.rpicamera.interface": {"ahwlan"},
		"camera-onvif-server.rpicamera.rtsp_name": {"camera/main"},
		"network.ahwlan.device":                   {"br-ahwlan"},
		"mediamtx.@mediamtx[0].rtsp_address":      {":8554"},
	})
	lookup := func(name string) network.NetworkInterface {
		assert.Equal(t, network.DefaultBridgeInterfaceName, name)

		return network.NetworkInterface{IP: []network.IPAddress{{IP: net.ParseIP("10.41.0.1")}}}
	}

	stream, err := resolveCameraStreamWith(context.Background(), reader, lookup)
	require.NoError(t, err)

	assert.Equal(t, Stream{Address: "10.41.0.1", Port: 8554, Path: "/camera/main"}, stream)
}

func TestResolveStream_defaults(t *testing.T) {
	t.Parallel()

	stream, err := resolveCameraStreamWith(context.Background(), newFakeConfigReader(t, nil), func(name string) network.NetworkInterface {
		assert.Equal(t, network.DefaultBridgeInterfaceName, name)

		return network.NetworkInterface{IP: []network.IPAddress{
			{IP: net.ParseIP("127.0.0.1")},
			{IP: net.ParseIP("10.41.2.3")},
		}}
	})
	require.NoError(t, err)
	assert.Equal(t, Stream{Address: "10.41.2.3", Port: defaultRTSPPort, Path: "/" + defaultRTSPName}, stream)
}

func TestResolveStream_missingIPv4Address(t *testing.T) {
	t.Parallel()

	_, err := resolveCameraStreamWith(context.Background(), newFakeConfigReader(t, nil), func(string) network.NetworkInterface {
		return network.NetworkInterface{IP: []network.IPAddress{{IP: net.ParseIP("127.0.0.1")}}}
	})
	require.Error(t, err)
}

func TestResolveStream_canceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lookupCalled := false
	_, err := resolveCameraStreamWith(ctx, newFakeConfigReader(t, nil), func(string) network.NetworkInterface {
		lookupCalled = true

		return network.NetworkInterface{}
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, lookupCalled)
}

func TestStream_URL(t *testing.T) {
	t.Parallel()

	stream := Stream{Address: "10.41.0.1", Path: "/rpicamera main", Port: 8554}
	assert.Equal(t, "rtsp://10.41.0.1:8554/rpicamera%20main", stream.URL())
}
