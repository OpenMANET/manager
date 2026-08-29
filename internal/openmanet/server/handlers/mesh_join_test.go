package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	wificonfigv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	"github.com/openmanet/openmanetd/internal/meshjoin"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// newMeshJoinReader seeds a HaLow mesh radio (radio3), a 2.4 GHz radio
// carrying the batmesh1 backhaul (radio0) and a 2.4 GHz client AP
// (radio2) — the shape a configured mesh-point extender has after the
// wizard ran with a backhaul.
func newMeshJoinReader() *fakeConfigReader {
	return &fakeConfigReader{
		data: map[string]map[string]map[string][]string{
			"wireless": {
				"radio0": {
					"type":    {"mac80211"},
					"band":    {"2g"},
					"channel": {"8"},
					"htmode":  {"HE20"},
					"country": {"US"},
					"txpower": {"20"},
				},
				"radio2": {
					"type":    {"mac80211"},
					"band":    {"5g"},
					"channel": {"36"},
					"htmode":  {"VHT80"},
					"country": {"US"},
					"txpower": {"20"},
				},
				"radio3": {
					"type":    {"morse"},
					"band":    {"s1g"},
					"channel": {"44"},
					"htmode":  {"8 MHz"},
					"country": {"US"},
					"txpower": {"14"},
				},
				"batmesh1_radio0": {
					"device":     {"radio0"},
					"network":    {"batmesh1"},
					"mode":       {"mesh"},
					"mesh_id":    {"field-mesh-2g"},
					"key":        {"backhaul-pass"},
					"encryption": {"sae"},
				},
				"default_radio2": {
					"device":     {"radio2"},
					"network":    {"ahwlan"},
					"mode":       {"ap"},
					"ssid":       {"openmanet"},
					"key":        {"clientsecret"},
					"encryption": {"psk2"},
				},
				"default_radio3": {
					"device":     {"radio3"},
					"network":    {"batmesh0"},
					"mode":       {"mesh"},
					"ssid":       {"field-mesh"},
					"mesh_id":    {"field-mesh"},
					"key":        {"correct-horse"},
					"encryption": {"sae"},
				},
			},
		},
		sectionTypes: map[string]map[string]string{
			"wireless": {
				"radio0":          "wifi-device",
				"radio2":          "wifi-device",
				"radio3":          "wifi-device",
				"batmesh1_radio0": "wifi-iface",
				"default_radio2":  "wifi-iface",
				"default_radio3":  "wifi-iface",
			},
		},
	}
}

func newTestMeshJoinService(reader *fakeConfigReader) *handlers.MeshJoinService {
	return &handlers.MeshJoinService{
		Log:          zerolog.Nop(),
		ConfigReader: reader,
		Hostname:     func() (string, error) { return "alpha", nil },
	}
}

func TestGetMeshJoinQR_HalowAndBackhaul(t *testing.T) {
	svc := newTestMeshJoinService(newMeshJoinReader())

	resp, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	p := resp.GetPayload()
	assert.Equal(t, "alpha", p.GetSourceHostname())

	h := p.GetHalow()
	require.NotNil(t, h)
	assert.Equal(t, "field-mesh", h.GetMeshId())
	assert.Equal(t, "correct-horse", h.GetPassphrase())
	assert.Equal(t, wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_SAE, h.GetEncryption())
	assert.Equal(t, uint32(8), h.GetBandwidthMhz())
	assert.Equal(t, uint32(44), h.GetChannel())
	assert.Equal(t, "US", h.GetCountryCode())

	b := p.GetBackhaul()
	require.NotNil(t, b, "the batmesh1 iface is the backhaul")
	assert.Equal(t, "field-mesh-2g", b.GetMeshId())
	assert.Equal(t, "backhaul-pass", b.GetPassphrase())
	assert.Equal(t, uint32(20), b.GetBandwidthMhz())
	assert.Equal(t, uint32(8), b.GetChannel())

	assert.True(t, strings.HasPrefix(resp.GetPayloadText(), meshjoin.Prefix))
	assert.True(t, strings.HasPrefix(resp.GetSvg(), "<svg "))

	decoded, err := meshjoin.Decode(resp.GetPayloadText())
	require.NoError(t, err)
	assert.Equal(t, "field-mesh", decoded.GetHalow().GetMeshId())
}

func TestGetMeshJoinQR_HalowOnly(t *testing.T) {
	reader := newMeshJoinReader()
	require.NoError(t, reader.DelSection("wireless", "batmesh1_radio0"))

	svc := newTestMeshJoinService(reader)

	resp, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Nil(t, resp.GetPayload().GetBackhaul(), "no 2.4 GHz mesh iface → no backhaul")
	assert.Equal(t, "field-mesh", resp.GetPayload().GetHalow().GetMeshId())
}

func TestGetMeshJoinQR_BackhaulOnNonBatmesh1Iface(t *testing.T) {
	reader := newMeshJoinReader()
	// Move the backhaul to a plain default_ section on a different
	// network name; it must still be picked up as the backhaul.
	require.NoError(t, reader.DelSection("wireless", "batmesh1_radio0"))
	require.NoError(t, reader.AddSection("wireless", "default_radio0", "wifi-iface"))

	for k, v := range map[string]string{
		"device": "radio0", "network": "batmesh0", "mode": "mesh",
		"mesh_id": "other-2g", "key": "backhaul-pass", "encryption": "sae",
	} {
		require.NoError(t, reader.SetType("wireless", "default_radio0", k, 0, v))
	}

	svc := newTestMeshJoinService(reader)

	resp, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "other-2g", resp.GetPayload().GetBackhaul().GetMeshId())
}

func TestGetMeshJoinQR_DisabledIfaceIgnored(t *testing.T) {
	reader := newMeshJoinReader()
	require.NoError(t, reader.SetType("wireless", "batmesh1_radio0", "disabled", 0, "1"))

	svc := newTestMeshJoinService(reader)

	resp, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Nil(t, resp.GetPayload().GetBackhaul())
}

func TestGetMeshJoinQR_NoHalowMesh(t *testing.T) {
	reader := newMeshJoinReader()
	require.NoError(t, reader.DelSection("wireless", "default_radio3"))

	svc := newTestMeshJoinService(reader)

	_, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})

	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Equal(t, connect.CodeFailedPrecondition, cerr.Code())
	assert.Contains(t, cerr.Message(), "no HaLow mesh interface")
}

func TestGetMeshJoinQR_OpenHalowMeshRejected(t *testing.T) {
	reader := newMeshJoinReader()
	require.NoError(t, reader.SetType("wireless", "default_radio3", "encryption", 0, "none"))
	require.NoError(t, reader.Del("wireless", "default_radio3", "key"))

	svc := newTestMeshJoinService(reader)

	_, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})

	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Equal(t, connect.CodeFailedPrecondition, cerr.Code())
	assert.Contains(t, cerr.Message(), "WPA3")
}

func TestGetMeshJoinQR_NonSAEBackhaulSkipped(t *testing.T) {
	reader := newMeshJoinReader()
	require.NoError(t, reader.SetType("wireless", "batmesh1_radio0", "encryption", 0, "psk2"))

	svc := newTestMeshJoinService(reader)

	resp, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})
	require.NoError(t, err, "the HaLow mesh still shares")
	assert.Nil(t, resp.GetPayload().GetBackhaul())
}

func TestGetMeshJoinQR_UnknownHTMode(t *testing.T) {
	reader := newMeshJoinReader()
	require.NoError(t, reader.SetType("wireless", "radio3", "htmode", 0, "EHT320"))

	svc := newTestMeshJoinService(reader)

	_, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})

	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Equal(t, connect.CodeInternal, cerr.Code())
}

func TestGetMeshJoinQR_HostnameError(t *testing.T) {
	svc := newTestMeshJoinService(newMeshJoinReader())
	svc.Hostname = func() (string, error) { return "", errors.New("gethostname: boom") }

	_, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})

	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Equal(t, connect.CodeInternal, cerr.Code())
}

func TestGetMeshJoinQR_HostnameTruncatedTo63(t *testing.T) {
	svc := newTestMeshJoinService(newMeshJoinReader())
	svc.Hostname = func() (string, error) { return strings.Repeat("h", 80), nil }

	resp, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Len(t, resp.GetPayload().GetSourceHostname(), 63)
}

func TestGetMeshJoinQR_NeverLogsSecrets(t *testing.T) {
	var buf bytes.Buffer

	svc := newTestMeshJoinService(newMeshJoinReader())
	svc.Log = zerolog.New(&buf).Level(zerolog.TraceLevel)

	resp, err := svc.GetMeshJoinQR(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	logs := buf.String()
	assert.NotContains(t, logs, "correct-horse")
	assert.NotContains(t, logs, "backhaul-pass")
	assert.NotContains(t, logs, resp.GetPayloadText())
}
