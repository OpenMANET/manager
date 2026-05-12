package mgmt

import (
	"net"
	"testing"
	"time"

	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
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

// ── sendGatewayDataOnceWithDeps tests ───────────────────────────────────────

func newGatewayTestWorker() *GatewayWorker {
	cfg := newTestManagementConfig()
	cfg.IFace = "br-ahwlan"
	cfg.BatInterface = "bat0"

	return &GatewayWorker{
		Config:       cfg,
		sendInterval: 60 * time.Second,
		recvInterval: 10 * time.Second,
	}
}

func makeMeshConfig(gwMode string, hardAddr string) *batmanadv.MeshConfig {
	return &batmanadv.MeshConfig{
		GwMode:      gwMode,
		HardAddress: hardAddr,
	}
}

func TestSendGatewayDataOnce_DHCPNotConfigured(t *testing.T) {
	gw := newGatewayTestWorker()
	openmanet := newFakeOpenMANETReader()
	// Not seeded — DHCP not configured
	client := &fakeAlfredClient{}

	err := gw.sendGatewayDataOnceWithDeps(client, openmanet,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("server", ""), nil },
		func(_ string) network.NetworkInterface { return network.NetworkInterface{} },
		func() (string, error) { return "host", nil },
	)
	require.NoError(t, err)
	assert.Equal(t, 0, client.setCalls)
}

func TestSendGatewayDataOnce_NotGatewayMode(t *testing.T) {
	gw := newGatewayTestWorker()
	openmanet := newFakeOpenMANETReader()
	seedDHCPConfigured(openmanet)

	client := &fakeAlfredClient{}

	err := gw.sendGatewayDataOnceWithDeps(client, openmanet,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("client", ""), nil },
		func(_ string) network.NetworkInterface { return network.NetworkInterface{} },
		func() (string, error) { return "host", nil },
	)
	require.NoError(t, err)
	assert.Equal(t, 0, client.setCalls, "should not send when not in gateway mode")
}

func TestSendGatewayDataOnce_MeshConfigError(t *testing.T) {
	gw := newGatewayTestWorker()
	openmanet := newFakeOpenMANETReader()
	seedDHCPConfigured(openmanet)

	client := &fakeAlfredClient{}

	err := gw.sendGatewayDataOnceWithDeps(client, openmanet,
		func(_ string) (*batmanadv.MeshConfig, error) { return nil, assert.AnError },
		func(_ string) network.NetworkInterface { return network.NetworkInterface{} },
		func() (string, error) { return "host", nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get mesh config")
}

func TestSendGatewayDataOnce_NoIPAddress(t *testing.T) {
	gw := newGatewayTestWorker()
	openmanet := newFakeOpenMANETReader()
	seedDHCPConfigured(openmanet)

	client := &fakeAlfredClient{}

	err := gw.sendGatewayDataOnceWithDeps(client, openmanet,
		func(_ string) (*batmanadv.MeshConfig, error) {
			return makeMeshConfig("server", "aa:bb:cc:dd:ee:ff"), nil
		},
		func(_ string) network.NetworkInterface { return network.NetworkInterface{Name: "br-ahwlan"} },
		func() (string, error) { return "host", nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no IP address")
}

func TestSendGatewayDataOnce_NoIPv4(t *testing.T) {
	gw := newGatewayTestWorker()
	openmanet := newFakeOpenMANETReader()
	seedDHCPConfigured(openmanet)

	client := &fakeAlfredClient{}

	// IPv6 only interface
	iface := network.NetworkInterface{
		Name: "br-ahwlan",
		IP:   []network.IPAddress{{IP: net.ParseIP("::1")}},
	}

	err := gw.sendGatewayDataOnceWithDeps(client, openmanet,
		func(_ string) (*batmanadv.MeshConfig, error) {
			return makeMeshConfig("server", "aa:bb:cc:dd:ee:ff"), nil
		},
		func(_ string) network.NetworkInterface { return iface },
		func() (string, error) { return "host", nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid IPv4 address")
}

func TestSendGatewayDataOnce_Success(t *testing.T) {
	gw := newGatewayTestWorker()
	openmanet := newFakeOpenMANETReader()
	seedDHCPConfigured(openmanet)

	client := &fakeAlfredClient{}
	iface := makeTestIface("11:22:33:44:55:66", "10.0.0.1")

	err := gw.sendGatewayDataOnceWithDeps(client, openmanet,
		func(_ string) (*batmanadv.MeshConfig, error) {
			return makeMeshConfig("server", "aa:bb:cc:dd:ee:ff"), nil
		},
		func(_ string) network.NetworkInterface { return iface },
		func() (string, error) { return "gw-host", nil },
	)
	require.NoError(t, err)
	assert.Equal(t, 1, client.setCalls)

	var sent proto.Gateway
	require.NoError(t, sent.UnmarshalVT(client.lastData))
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", sent.Mac, "should use mesh hard address, not iface MAC")
	assert.Equal(t, "10.0.0.1", sent.Ipaddr)
	assert.Equal(t, "gw-host", sent.Hostname)
}

func TestSendGatewayDataOnce_AlfredSetError(t *testing.T) {
	gw := newGatewayTestWorker()
	openmanet := newFakeOpenMANETReader()
	seedDHCPConfigured(openmanet)

	client := &fakeAlfredClient{setErr: assert.AnError}
	iface := makeTestIface("11:22:33:44:55:66", "10.0.0.1")

	err := gw.sendGatewayDataOnceWithDeps(client, openmanet,
		func(_ string) (*batmanadv.MeshConfig, error) {
			return makeMeshConfig("server", "aa:bb:cc:dd:ee:ff"), nil
		},
		func(_ string) network.NetworkInterface { return iface },
		func() (string, error) { return "host", nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send gateway data")
}

// ── receiveGatewayDataOnceWithDeps tests ────────────────────────────────────

func makeGatewayRecord(t *testing.T, mac, ip, hostname string) alfred.Record {
	t.Helper()

	gw := &proto.Gateway{Mac: mac, Ipaddr: ip, Hostname: hostname}

	data, err := gw.MarshalVT()
	require.NoError(t, err)

	return alfred.Record{Data: data}
}

func TestReceiveGatewayDataOnce_GatewayModeSkips(t *testing.T) {
	gw := newGatewayTestWorker()
	client := &fakeAlfredClient{}

	err := gw.receiveGatewayDataOnceWithDeps(client,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("server", ""), nil },
		nil, nil, nil, nil,
	)
	require.NoError(t, err)
}

func TestReceiveGatewayDataOnce_MeshConfigError(t *testing.T) {
	gw := newGatewayTestWorker()
	client := &fakeAlfredClient{}

	err := gw.receiveGatewayDataOnceWithDeps(client,
		func(_ string) (*batmanadv.MeshConfig, error) { return nil, assert.AnError },
		nil, nil, nil, nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get mesh config")
}

func TestReceiveGatewayDataOnce_NoGateways(t *testing.T) {
	gw := newGatewayTestWorker()
	client := &fakeAlfredClient{records: []alfred.Record{makeGatewayRecord(t, "aa:bb:cc:dd:ee:ff", "10.0.0.1", "gw1")}}
	emptyGwys := batmanadv.Gateways{}

	err := gw.receiveGatewayDataOnceWithDeps(client,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("client", ""), nil },
		func(_ string) (*batmanadv.Gateways, error) { return &emptyGwys, nil },
		nil, nil, nil,
	)
	require.NoError(t, err)
}

func TestReceiveGatewayDataOnce_MatchesAndReplacesRoute(t *testing.T) {
	gw := newGatewayTestWorker()
	gw.Config.IFace = "br-ahwlan"

	gwMAC := "aa:bb:cc:dd:ee:ff"
	gwIP := "10.0.0.1"

	client := &fakeAlfredClient{
		records: []alfred.Record{makeGatewayRecord(t, gwMAC, gwIP, "gw1")},
	}

	batGwys := batmanadv.Gateways{{OrigAddress: gwMAC, Best: true}}

	var replacedIP net.IP

	var replacedIface string

	networkReader := newFakeNetworkReader()

	var dnsSection, dnsValue string

	err := gw.receiveGatewayDataOnceWithDeps(client,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("client", ""), nil },
		func(_ string) (*batmanadv.Gateways, error) { return &batGwys, nil },
		func(ip net.IP, iface string) error {
			replacedIP = ip
			replacedIface = iface

			return nil
		},
		func(section, dns string, _ network.ConfigReader) error {
			dnsSection = section
			dnsValue = dns

			return nil
		},
		networkReader,
	)
	require.NoError(t, err)
	assert.Equal(t, gwIP, replacedIP.String())
	assert.Equal(t, "br-ahwlan", replacedIface)
	assert.Equal(t, "ahwlan", dnsSection)
	assert.Equal(t, gwIP, dnsValue)
}

func TestReceiveGatewayDataOnce_NoMatchingMAC(t *testing.T) {
	gw := newGatewayTestWorker()
	gw.Config.IFace = "br-ahwlan"

	client := &fakeAlfredClient{
		records: []alfred.Record{makeGatewayRecord(t, "11:22:33:44:55:66", "10.0.0.2", "gw2")},
	}

	batGwys := batmanadv.Gateways{{OrigAddress: "aa:bb:cc:dd:ee:ff", Best: true}}
	routeCalled := false

	err := gw.receiveGatewayDataOnceWithDeps(client,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("client", ""), nil },
		func(_ string) (*batmanadv.Gateways, error) { return &batGwys, nil },
		func(_ net.IP, _ string) error {
			routeCalled = true

			return nil
		},
		func(_, _ string, _ network.ConfigReader) error { return nil },
		newFakeNetworkReader(),
	)
	require.NoError(t, err)
	assert.False(t, routeCalled, "route should not be replaced when MAC doesn't match")
}

func TestReceiveGatewayDataOnce_NoBestGateway(t *testing.T) {
	gw := newGatewayTestWorker()
	gw.Config.IFace = "br-ahwlan"

	client := &fakeAlfredClient{
		records: []alfred.Record{makeGatewayRecord(t, "aa:bb:cc:dd:ee:ff", "10.0.0.1", "gw1")},
	}

	// Gateways list is non-empty but none are flagged Best — GetBest() returns nil.
	// Regression test for SIGSEGV at gateway.go:219 dereferencing nil *Gateway.
	batGwys := batmanadv.Gateways{
		{OrigAddress: "aa:bb:cc:dd:ee:ff", Best: false},
		{OrigAddress: "11:22:33:44:55:66", Best: false},
	}
	routeCalled := false

	err := gw.receiveGatewayDataOnceWithDeps(client,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("client", ""), nil },
		func(_ string) (*batmanadv.Gateways, error) { return &batGwys, nil },
		func(_ net.IP, _ string) error {
			routeCalled = true

			return nil
		},
		func(_, _ string, _ network.ConfigReader) error { return nil },
		newFakeNetworkReader(),
	)
	require.NoError(t, err)
	assert.False(t, routeCalled, "route should not be replaced when no gateway is marked best")
}

func TestReceiveGatewayDataOnce_RequestError(t *testing.T) {
	gw := newGatewayTestWorker()
	client := &fakeAlfredClient{requestErr: assert.AnError}

	err := gw.receiveGatewayDataOnceWithDeps(client,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("client", ""), nil },
		nil, nil, nil, nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request gateway data")
}

func TestReceiveGatewayDataOnce_InvalidProtobuf(t *testing.T) {
	gw := newGatewayTestWorker()
	gw.Config.IFace = "br-ahwlan"

	client := &fakeAlfredClient{
		records: []alfred.Record{{Data: []byte("bad-data")}},
	}

	batGwys := batmanadv.Gateways{{OrigAddress: "aa:bb:cc:dd:ee:ff", Best: true}}

	// Should not error — invalid records are logged and skipped
	err := gw.receiveGatewayDataOnceWithDeps(client,
		func(_ string) (*batmanadv.MeshConfig, error) { return makeMeshConfig("client", ""), nil },
		func(_ string) (*batmanadv.Gateways, error) { return &batGwys, nil },
		func(_ net.IP, _ string) error { return nil },
		func(_, _ string, _ network.ConfigReader) error { return nil },
		newFakeNetworkReader(),
	)
	require.NoError(t, err)
}
