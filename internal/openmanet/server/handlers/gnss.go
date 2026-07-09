package handlers

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	gnssv1 "github.com/openmanet/openmanetd/internal/api/openmanet/gnss/v1"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GNSSProvider abstracts GPS data retrieval for testability.
type GNSSProvider interface {
	GetPosition() gpsd.PositionReport
	GetSatelliteReport() gpsd.SatelliteReport
}

// GNSSService implements the gnssv1connect.GNSSServiceHandler interface.
type GNSSService struct {
	Cfg *config.Config
	Log zerolog.Logger
	GPS GNSSProvider // nil when GNSS hardware not available
}

// GetGNSSStatus returns the current GPS position and satellite data.
func (g *GNSSService) GetGNSSStatus(_ context.Context, _ *emptypb.Empty) (*gnssv1.GetGNSSStatusResponse, error) {
	resp := &gnssv1.GetGNSSStatusResponse{}

	if g.GPS == nil {
		return resp, nil
	}

	pos := g.GPS.GetPosition()
	satReport := g.GPS.GetSatelliteReport()

	resp.Position = &gnssv1.Position{
		FixType:   modeToFixType(pos.Mode),
		Latitude:  pos.Latitude,
		Longitude: pos.Longitude,
		Altitude:  pos.Altitude,
		Speed:     pos.Speed,
		Heading:   pos.Track,
		Pdop:      satReport.PDOP,
		Hdop:      pos.HDOP,
	}

	if !pos.Timestamp.IsZero() {
		resp.Position.LastUpdate = timestamppb.New(pos.Timestamp)
	}

	sats := make([]*gnssv1.Satellite, 0, len(satReport.Satellites))
	for _, s := range satReport.Satellites {
		sats = append(sats, &gnssv1.Satellite{
			Prn:           int32(s.PRN),
			Elevation:     s.El,
			Azimuth:       s.Az,
			Snr:           s.Ss,
			Used:          s.Used,
			Constellation: gnssIDToConstellation(s.Gnssid),
		})
	}

	resp.SatelliteStatus = &gnssv1.SatelliteStatus{
		SatellitesUsed:   int32(satReport.USat),
		SatellitesInView: int32(satReport.NSat),
		Satellites:       sats,
	}

	if !satReport.Timestamp.IsZero() {
		resp.SatelliteStatus.LastUpdate = timestamppb.New(satReport.Timestamp)
	}

	return resp, nil
}

// GetGNSSConfig returns the current GPS settings and output protocol configuration.
func (g *GNSSService) GetGNSSConfig(_ context.Context, _ *emptypb.Empty) (*gnssv1.GetGNSSConfigResponse, error) {
	return &gnssv1.GetGNSSConfigResponse{
		Settings: &gnssv1.GNSSSettings{
			EnableGps: g.Cfg.GetEnableGNSS(),
			Source:    sourceStringToProto(g.Cfg.GetGNSSSource()),
		},
		OutputProtocols: &gnssv1.OutputProtocols{
			SendAsNmea: g.Cfg.GetGNSSSendAsNMEA(),
			SendAsCot:  g.Cfg.GetGNSSSendAsCoT(),
			CotUid:     g.Cfg.GetGNSSCoTUID(),
		},
	}, nil
}

// UpdateGNSSConfig updates GPS settings and output protocol configuration.
func (g *GNSSService) UpdateGNSSConfig(_ context.Context, req *gnssv1.UpdateGNSSConfigRequest) (*gnssv1.UpdateGNSSConfigResponse, error) {
	settings := req.GetSettings()
	output := req.GetOutputProtocols()

	if err := g.Cfg.PersistGNSSConfig(
		settings.GetEnableGps(),
		output.GetSendAsNmea(),
		output.GetSendAsCot(),
		output.GetCotUid(),
		sourceProtoToString(settings.GetSource()),
	); err != nil {
		g.Log.Error().Err(err).Msg("Failed to persist GNSS config")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to persist GNSS config: %w", err))
	}

	message := "GNSS configuration updated successfully."

	return &gnssv1.UpdateGNSSConfigResponse{
		Success: true,
		Message: &message,
	}, nil
}

// sourceStringToProto maps the persisted config string to the GNSSSource
// enum. Unknown values fall back to GNSS_SOURCE_INTERNAL.
func sourceStringToProto(source string) gnssv1.GNSSSource {
	if source == "external_cot" {
		return gnssv1.GNSSSource_GNSS_SOURCE_EXTERNAL_COT
	}

	return gnssv1.GNSSSource_GNSS_SOURCE_INTERNAL
}

// sourceProtoToString maps the GNSSSource enum to the string persisted in
// the config file. GNSS_SOURCE_UNSPECIFIED is treated as internal.
func sourceProtoToString(source gnssv1.GNSSSource) string {
	if source == gnssv1.GNSSSource_GNSS_SOURCE_EXTERNAL_COT {
		return "external_cot"
	}

	return "internal"
}

// modeToFixType maps GPSD mode values to the protobuf FixType enum.
func modeToFixType(mode int) gnssv1.FixType {
	switch mode {
	case 2:
		return gnssv1.FixType_FIX_TYPE_2D
	case 3:
		return gnssv1.FixType_FIX_TYPE_3D
	default:
		return gnssv1.FixType_FIX_TYPE_NO_FIX
	}
}

// gnssIDToConstellation maps gpsd's gnssid encoding to the protobuf
// Constellation enum. Unknown ids fall back to UNSPECIFIED.
func gnssIDToConstellation(id int) gnssv1.Constellation {
	switch id {
	case 0:
		return gnssv1.Constellation_CONSTELLATION_GPS
	case 1:
		return gnssv1.Constellation_CONSTELLATION_SBAS
	case 2:
		return gnssv1.Constellation_CONSTELLATION_GALILEO
	case 3:
		return gnssv1.Constellation_CONSTELLATION_BEIDOU
	case 4:
		return gnssv1.Constellation_CONSTELLATION_IMES
	case 5:
		return gnssv1.Constellation_CONSTELLATION_QZSS
	case 6:
		return gnssv1.Constellation_CONSTELLATION_GLONASS
	case 7:
		return gnssv1.Constellation_CONSTELLATION_IRNSS
	default:
		return gnssv1.Constellation_CONSTELLATION_UNSPECIFIED
	}
}
