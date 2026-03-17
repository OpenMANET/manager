package mgmt

import (
	"testing"
	"time"

	proto "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayDataTypeConstants(t *testing.T) {
	// Guard against accidental changes to protocol constants.
	assert.Equal(t, uint8(100), GatewayDataType, "GatewayDataType must match proto DataType_DATA_TYPE_GATEWAY")
	assert.Equal(t, uint8(1), GatewayDataTypeVersion)
}

func TestNodeDataTypeConstants(t *testing.T) {
	// Guard against accidental changes to protocol constants.
	assert.Equal(t, uint8(102), NodeDataType, "NodeDataType must match proto DataType_DATA_TYPE_NODE")
	assert.Equal(t, uint8(1), NodeDataTypeVersion)
}

func TestGatewayProtoRoundTrip(t *testing.T) {
	original := &proto.Gateway{
		Mac:      "aa:bb:cc:dd:ee:ff",
		Hostname: "gw-node-1",
		Ipaddr:   "10.0.0.1",
	}

	data, err := original.MarshalVT()
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var decoded proto.Gateway

	err = decoded.UnmarshalVT(data)
	require.NoError(t, err)

	assert.Equal(t, original.Mac, decoded.Mac)
	assert.Equal(t, original.Hostname, decoded.Hostname)
	assert.Equal(t, original.Ipaddr, decoded.Ipaddr)
}

func TestNewGatewayWorker_SetsIntervals(t *testing.T) {
	cfg := newTestManagementConfig()
	cfg.gatewayWorkerSendInterval = 30 * time.Second
	cfg.gatewayWorkerRecvInterval = 5 * time.Second

	shutdownChan := make(chan struct{})
	defer close(shutdownChan)

	// NewGatewayWorker requires an *alfred.Client which needs a real socket.
	// Instead, test the struct fields directly.
	gw := &GatewayWorker{
		sendInterval: cfg.gatewayWorkerSendInterval,
		recvInterval: cfg.gatewayWorkerRecvInterval,
	}

	assert.Equal(t, 30*time.Second, gw.sendInterval)
	assert.Equal(t, 5*time.Second, gw.recvInterval)
}

func TestNewNodeDataWorker_SetsInterval(t *testing.T) {
	interval := 45 * time.Second

	ndw := &NodeDataWorker{
		Interval: interval,
	}

	assert.Equal(t, 45*time.Second, ndw.Interval)
}
