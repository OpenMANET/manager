package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	gnssv1 "github.com/openmanet/openmanetd/internal/api/openmanet/gnss/v1"
	gnssconnect "github.com/openmanet/openmanetd/internal/api/openmanet/gnss/v1/gnssv1connect"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ── helpers ─────────────────────────────────────────────────────────────────

func newGNSSService(gps handlers.GNSSProvider, cfg *config.Config) *handlers.GNSSService {
	if cfg == nil {
		cfg = config.NewWithoutWatch(viper.New())
	}

	return &handlers.GNSSService{
		Cfg: cfg,
		Log: zerolog.Nop(),
		GPS: gps,
	}
}

func setupGNSSTestConfig(t *testing.T, yamlContent string) *config.Config {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)

	err = v.ReadInConfig()
	require.NoError(t, err)

	return config.NewWithoutWatch(v)
}

// ── GetGNSSStatus tests ─────────────────────────────────────────────────────

func TestGetGNSSStatus_NilGPS(t *testing.T) {
	svc := newGNSSService(nil, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.Nil(t, resp.Position)
	assert.Nil(t, resp.SatelliteStatus)
}

func TestGetGNSSStatus_NoFix(t *testing.T) {
	fake := &fakeGNSSProvider{
		position: gpsd.PositionReport{Mode: 0},
	}
	svc := newGNSSService(fake, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.Equal(t, gnssv1.FixType_FIX_TYPE_NO_FIX, resp.Position.FixType)
}

func TestGetGNSSStatus_2DFix(t *testing.T) {
	fake := &fakeGNSSProvider{
		position: gpsd.PositionReport{
			Valid:     true,
			Mode:      2,
			Latitude:  34.0522,
			Longitude: -118.2437,
			Speed:     5.5,
			Track:     180.0,
			HDOP:      1.2,
		},
	}
	svc := newGNSSService(fake, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.Equal(t, gnssv1.FixType_FIX_TYPE_2D, resp.Position.FixType)
	assert.InDelta(t, 34.0522, resp.Position.Latitude, 0.0001)
	assert.InDelta(t, -118.2437, resp.Position.Longitude, 0.0001)
	assert.InDelta(t, 5.5, resp.Position.Speed, 0.01)
	assert.InDelta(t, 180.0, resp.Position.Heading, 0.01)
	assert.InDelta(t, 1.2, resp.Position.Hdop, 0.01)
}

func TestGetGNSSStatus_3DFix(t *testing.T) {
	fake := &fakeGNSSProvider{
		position: gpsd.PositionReport{
			Valid:    true,
			Mode:     3,
			Latitude: 37.7749,
			Altitude: 71.0,
		},
	}
	svc := newGNSSService(fake, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.Equal(t, gnssv1.FixType_FIX_TYPE_3D, resp.Position.FixType)
	assert.InDelta(t, 71.0, resp.Position.Altitude, 0.01)
}

func TestGetGNSSStatus_WithSatellites(t *testing.T) {
	fake := &fakeGNSSProvider{
		position: gpsd.PositionReport{Mode: 3, Valid: true},
		satelliteReport: gpsd.SatelliteReport{
			USat: 3,
			NSat: 5,
			Satellites: []gpsd.SatelliteInfo{
				{PRN: 2, El: 45.0, Az: 120.0, Ss: 38.0, Used: true},
				{PRN: 7, El: 15.0, Az: 330.0, Ss: 18.0, Used: false},
				{PRN: 15, El: 80.0, Az: 90.0, Ss: 44.0, Used: true},
			},
		},
	}
	svc := newGNSSService(fake, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	require.Len(t, resp.SatelliteStatus.Satellites, 3)

	sat := resp.SatelliteStatus.Satellites[0]
	assert.Equal(t, int32(2), sat.Prn)
	assert.InDelta(t, 45.0, sat.Elevation, 0.01)
	assert.InDelta(t, 120.0, sat.Azimuth, 0.01)
	assert.InDelta(t, 38.0, sat.Snr, 0.01)
	assert.True(t, sat.Used)

	sat2 := resp.SatelliteStatus.Satellites[1]
	assert.Equal(t, int32(7), sat2.Prn)
	assert.False(t, sat2.Used)
}

func TestGetGNSSStatus_SatelliteCounts(t *testing.T) {
	fake := &fakeGNSSProvider{
		position: gpsd.PositionReport{Mode: 3, Valid: true},
		satelliteReport: gpsd.SatelliteReport{
			USat: 8,
			NSat: 12,
		},
	}
	svc := newGNSSService(fake, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.Equal(t, int32(8), resp.SatelliteStatus.SatellitesUsed)
	assert.Equal(t, int32(12), resp.SatelliteStatus.SatellitesInView)
}

func TestGetGNSSStatus_PDOPFromSatReport(t *testing.T) {
	fake := &fakeGNSSProvider{
		position:        gpsd.PositionReport{Mode: 3, Valid: true, HDOP: 0.9},
		satelliteReport: gpsd.SatelliteReport{PDOP: 1.2, HDOP: 0.9},
	}
	svc := newGNSSService(fake, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.InDelta(t, 1.2, resp.Position.Pdop, 0.01)
	assert.InDelta(t, 0.9, resp.Position.Hdop, 0.01)
}

func TestGetGNSSStatus_ZeroTimestamp(t *testing.T) {
	fake := &fakeGNSSProvider{
		position: gpsd.PositionReport{Mode: 3, Valid: true},
	}
	svc := newGNSSService(fake, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.Nil(t, resp.Position.LastUpdate)
}

func TestGetGNSSStatus_ValidTimestamp(t *testing.T) {
	ts := time.Date(2026, 4, 1, 9, 42, 15, 0, time.UTC)
	fake := &fakeGNSSProvider{
		position: gpsd.PositionReport{Mode: 3, Valid: true, Timestamp: ts},
	}
	svc := newGNSSService(fake, nil)
	resp, err := svc.GetGNSSStatus(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	require.NotNil(t, resp.Position.LastUpdate)
	assert.Equal(t, ts.Unix(), resp.Position.LastUpdate.AsTime().Unix())
}

// ── GetGNSSConfig tests ─────────────────────────────────────────────────────

func TestGetGNSSConfig_Defaults(t *testing.T) {
	svc := newGNSSService(nil, nil)
	resp, err := svc.GetGNSSConfig(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.False(t, resp.Settings.EnableGps)
	assert.False(t, resp.OutputProtocols.SendAsNmea)
	assert.False(t, resp.OutputProtocols.SendAsCot)
	assert.Empty(t, resp.OutputProtocols.CotUid)
}

func TestGetGNSSConfig_FromConfig(t *testing.T) {
	cfg := setupGNSSTestConfig(t, `
gnss:
  enable: true
  sendAsExternalGNSSSource:
    sendAsNMEA: true
    sendAsCoT: true
    cotUID: "my-device-123"
`)
	svc := newGNSSService(nil, cfg)
	resp, err := svc.GetGNSSConfig(t.Context(), &emptypb.Empty{})

	require.NoError(t, err)
	assert.True(t, resp.Settings.EnableGps)
	assert.True(t, resp.OutputProtocols.SendAsNmea)
	assert.True(t, resp.OutputProtocols.SendAsCot)
	assert.Equal(t, "my-device-123", resp.OutputProtocols.CotUid)
}

// ── UpdateGNSSConfig tests ──────────────────────────────────────────────────

func TestUpdateGNSSConfig_Success(t *testing.T) {
	cfg := setupGNSSTestConfig(t, `
gnss:
  enable: false
  sendAsExternalGNSSSource:
    sendAsNMEA: false
    sendAsCoT: false
    cotUID: ""
`)
	svc := newGNSSService(nil, cfg)
	resp, err := svc.UpdateGNSSConfig(t.Context(), &gnssv1.UpdateGNSSConfigRequest{
		Settings:        &gnssv1.GNSSSettings{EnableGps: true},
		OutputProtocols: &gnssv1.OutputProtocols{SendAsNmea: true, SendAsCot: true, CotUid: "test-uid"},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Contains(t, *resp.Message, "updated successfully")

	// Verify config was updated in memory
	assert.True(t, cfg.GetEnableGNSS())
	assert.True(t, cfg.GetGNSSSendAsNMEA())
	assert.True(t, cfg.GetGNSSSendAsCoT())
	assert.Equal(t, "test-uid", cfg.GetGNSSCoTUID())
}

func TestUpdateGNSSConfig_PersistError(t *testing.T) {
	// Config with no file path will fail to persist
	v := viper.New()
	cfg := config.NewWithoutWatch(v)

	svc := newGNSSService(nil, cfg)
	_, err := svc.UpdateGNSSConfig(t.Context(), &gnssv1.UpdateGNSSConfigRequest{
		Settings:        &gnssv1.GNSSSettings{EnableGps: true},
		OutputProtocols: &gnssv1.OutputProtocols{},
	})

	require.Error(t, err)

	var connectErr *connect.Error

	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
}

// ── Integration tests ───────────────────────────────────────────────────────

func newGNSSTestServer(t *testing.T, cfg *config.Config, gps handlers.GNSSProvider) *httptest.Server {
	t.Helper()

	validateInterceptor := validate.NewInterceptor()
	handlerOpt := connect.WithInterceptors(validateInterceptor)

	mux := http.NewServeMux()
	mux.Handle(gnssconnect.NewGNSSServiceHandler(&handlers.GNSSService{
		Cfg: cfg,
		Log: zerolog.Nop(),
		GPS: gps,
	}, handlerOpt))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestIntegration_GetGNSSStatus_WithSatellites(t *testing.T) {
	fake := &fakeGNSSProvider{
		position: gpsd.PositionReport{
			Valid:     true,
			Mode:      3,
			Latitude:  37.7749,
			Longitude: -122.4194,
			Altitude:  10.5,
			Speed:     5.2,
			Track:     45.0,
			HDOP:      1.2,
			Timestamp: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		},
		satelliteReport: gpsd.SatelliteReport{
			PDOP: 2.4,
			HDOP: 1.2,
			VDOP: 1.8,
			NSat: 12,
			USat: 8,
			Satellites: []gpsd.SatelliteInfo{
				{PRN: 1, El: 45.0, Az: 180.0, Ss: 35.0, Used: true},
				{PRN: 5, El: 72.0, Az: 210.0, Ss: 42.0, Used: true},
			},
		},
	}

	srv := newGNSSTestServer(t, config.NewWithoutWatch(viper.New()), fake)
	client := gnssconnect.NewGNSSServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetGNSSStatus(t.Context(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.Equal(t, gnssv1.FixType_FIX_TYPE_3D, resp.Position.FixType)
	assert.InDelta(t, 37.7749, resp.Position.Latitude, 0.0001)
	assert.InDelta(t, -122.4194, resp.Position.Longitude, 0.0001)
	assert.InDelta(t, 2.4, resp.Position.Pdop, 0.01)
	require.NotNil(t, resp.Position.LastUpdate)

	assert.Equal(t, int32(8), resp.SatelliteStatus.SatellitesUsed)
	assert.Equal(t, int32(12), resp.SatelliteStatus.SatellitesInView)
	require.Len(t, resp.SatelliteStatus.Satellites, 2)
	assert.Equal(t, int32(1), resp.SatelliteStatus.Satellites[0].Prn)
	assert.True(t, resp.SatelliteStatus.Satellites[0].Used)
}

func TestIntegration_GetGNSSConfig_Defaults(t *testing.T) {
	srv := newGNSSTestServer(t, config.NewWithoutWatch(viper.New()), nil)
	client := gnssconnect.NewGNSSServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetGNSSConfig(t.Context(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.False(t, resp.Settings.EnableGps)
	assert.False(t, resp.OutputProtocols.SendAsNmea)
	assert.False(t, resp.OutputProtocols.SendAsCot)
	assert.Empty(t, resp.OutputProtocols.CotUid)
}

func TestIntegration_UpdateGNSSConfig_PersistsAndVerify(t *testing.T) {
	cfg := setupGNSSTestConfig(t, `
gnss:
  enable: false
  sendAsExternalGNSSSource:
    sendAsNMEA: false
    sendAsCoT: false
    cotUID: ""
`)

	srv := newGNSSTestServer(t, cfg, nil)
	client := gnssconnect.NewGNSSServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	// Update config
	updateResp, err := client.UpdateGNSSConfig(t.Context(), &gnssv1.UpdateGNSSConfigRequest{
		Settings:        &gnssv1.GNSSSettings{EnableGps: true},
		OutputProtocols: &gnssv1.OutputProtocols{SendAsNmea: true, SendAsCot: true, CotUid: "integration-test"},
	})
	require.NoError(t, err)
	assert.True(t, updateResp.Success)

	// Verify via GetGNSSConfig
	getResp, err := client.GetGNSSConfig(t.Context(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, getResp.Settings.EnableGps)
	assert.True(t, getResp.OutputProtocols.SendAsNmea)
	assert.True(t, getResp.OutputProtocols.SendAsCot)
	assert.Equal(t, "integration-test", getResp.OutputProtocols.CotUid)
}
