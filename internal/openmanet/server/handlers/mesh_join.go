package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	meshjoinv1 "github.com/openmanet/openmanetd/internal/api/openmanet/mesh_join/v1"
	wificonfigv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	"github.com/openmanet/openmanetd/internal/meshjoin"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	// maxSourceHostname mirrors MeshJoinPayload.source_hostname's max_len.
	maxSourceHostname = 63

	uciTypeMorse = "morse"
)

// MeshJoinService implements meshjoinconnect.MeshJoinServiceHandler:
// it shares this node's mesh credentials as a QR payload and joins the
// node to meshes described by a scanned payload.
type MeshJoinService struct {
	Log          zerolog.Logger
	ConfigReader network.ConfigReader
	// Hostname can be overridden for tests; nil falls back to os.Hostname.
	Hostname func() (string, error)
}

// meshIface is one enabled mesh-mode wifi-iface joined with its radio.
type meshIface struct {
	device  *network.UCIWirelessDevice
	iface   *network.UCIWirelessIface
	section string
	radio   string
	network string
	isMorse bool
}

// GetMeshJoinQR renders this node's mesh credentials as a QR code.
func (s *MeshJoinService) GetMeshJoinQR(_ context.Context, _ *emptypb.Empty) (*meshjoinv1.GetMeshJoinQRResponse, error) {
	s.Log.Debug().Msg("GetMeshJoinQR request received")

	ifaces, err := s.listMeshIfaces()
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to read mesh interfaces")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	halow := findMeshIface(ifaces, func(mi meshIface) bool { return mi.isMorse })
	if halow == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("no HaLow mesh interface configured"))
	}

	halowCreds, err := credentialsFromIface(*halow)
	if err != nil {
		s.Log.Error().Err(err).Str("section", halow.section).Msg("Failed to read HaLow mesh credentials")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if validateErr := validateCredentials("HaLow mesh", halowCreds); validateErr != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("%w; QR sharing needs a WPA3 (SAE) mesh with a passphrase", validateErr))
	}

	payload := &meshjoinv1.MeshJoinPayload{Halow: halowCreds}

	if bh := pickBackhaulIface(ifaces); bh != nil {
		creds, credErr := credentialsFromIface(*bh)
		if credErr != nil {
			s.Log.Warn().Err(credErr).Str("section", bh.section).Msg("Skipping backhaul in QR payload")
		} else if validateErr := validateCredentials("backhaul mesh", creds); validateErr != nil {
			s.Log.Warn().Err(validateErr).Str("section", bh.section).Msg("Skipping backhaul in QR payload")
		} else {
			payload.Backhaul = creds
		}
	}

	hostname, err := s.hostname()
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to read hostname")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read hostname: %w", err))
	}

	if len(hostname) > maxSourceHostname {
		hostname = hostname[:maxSourceHostname]
	}

	payload.SourceHostname = hostname

	text, err := meshjoin.Encode(payload)
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to encode mesh join payload")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	svg, err := meshjoin.RenderSVG(text)
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to render mesh join QR")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return &meshjoinv1.GetMeshJoinQRResponse{
		Payload:     payload,
		PayloadText: text,
		Svg:         svg,
	}, nil
}

// ApplyMeshJoin is implemented in a later task.
func (s *MeshJoinService) ApplyMeshJoin(_ context.Context, _ *meshjoinv1.ApplyMeshJoinRequest) (*meshjoinv1.ApplyMeshJoinResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("ApplyMeshJoin is not available yet"))
}

func (s *MeshJoinService) hostname() (string, error) {
	if s.Hostname != nil {
		return s.Hostname()
	}

	return os.Hostname() //nolint:wrapcheck // caller wraps
}

// listMeshIfaces returns every enabled mesh-mode wifi-iface joined with
// its wifi-device, sorted by section name so target selection is
// deterministic regardless of UCI iteration order.
func (s *MeshJoinService) listMeshIfaces() ([]meshIface, error) {
	sections, err := s.ConfigReader.GetSections(wirelessConfig, "wifi-iface")
	if err != nil {
		return nil, fmt.Errorf("list wifi-iface sections: %w", err)
	}

	out := make([]meshIface, 0, len(sections))

	for _, sec := range sections {
		iface, err := network.GetWirelessIfaceByNameWithReader(sec, s.ConfigReader)
		if err != nil {
			return nil, fmt.Errorf("read iface %s: %w", sec, err)
		}

		if iface.Mode != uciModeMesh || iface.Disabled == "1" || iface.Device == "" {
			continue
		}

		dev, err := network.GetWirelessDeviceByNameWithReader(iface.Device, s.ConfigReader)
		if err != nil {
			return nil, fmt.Errorf("read device %s: %w", iface.Device, err)
		}

		out = append(out, meshIface{
			section: sec,
			radio:   iface.Device,
			network: iface.Network,
			isMorse: strings.EqualFold(dev.Type, uciTypeMorse),
			device:  dev,
			iface:   iface,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].section < out[j].section })

	return out, nil
}

func findMeshIface(ifaces []meshIface, match func(meshIface) bool) *meshIface {
	for i := range ifaces {
		if match(ifaces[i]) {
			return &ifaces[i]
		}
	}

	return nil
}

// pickBackhaulIface prefers the mesh iface bound to the secondary
// batadv hardif, then any other non-HaLow mesh iface.
func pickBackhaulIface(ifaces []meshIface) *meshIface {
	if mi := findMeshIface(ifaces, func(mi meshIface) bool {
		return !mi.isMorse && mi.network == network.BatmanSecondaryIface
	}); mi != nil {
		return mi
	}

	return findMeshIface(ifaces, func(mi meshIface) bool { return !mi.isMorse })
}

// credentialsFromIface lifts the UCI iface + device options into a
// MeshCredentials message.
func credentialsFromIface(mi meshIface) (*meshjoinv1.MeshCredentials, error) {
	bw, ok := network.HTModeBandwidthMHz(mi.device.HTMode)
	if !ok {
		return nil, fmt.Errorf("radio %s: unknown htmode %q", mi.radio, mi.device.HTMode)
	}

	ch, err := strconv.ParseUint(mi.device.Channel, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("radio %s: channel %q is not a number: %w", mi.radio, mi.device.Channel, err)
	}

	return &meshjoinv1.MeshCredentials{
		MeshId:       mi.iface.MeshID,
		Passphrase:   mi.iface.Key,
		Encryption:   WifiEncryptionToProto(mi.iface.Encryption),
		BandwidthMhz: bw,
		Channel:      uint32(ch),
		CountryCode:  strings.ToUpper(mi.device.Country),
	}, nil
}

// validateCredentials applies the rules shared by both RPCs: SAE only,
// a passphrase of 8..63 characters, a mesh ID of 1..32 characters and a
// channel. Regulatory checks are separate.
func validateCredentials(label string, c *meshjoinv1.MeshCredentials) error {
	switch {
	case c == nil:
		return fmt.Errorf("%s: credentials are required", label)
	case c.GetEncryption() != wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_SAE:
		return fmt.Errorf("%s: mesh is not WPA3 (SAE)", label)
	case len(c.GetPassphrase()) < 8 || len(c.GetPassphrase()) > 63:
		return fmt.Errorf("%s: passphrase must be 8..63 characters", label)
	case c.GetMeshId() == "" || len(c.GetMeshId()) > 32:
		return fmt.Errorf("%s: mesh ID must be 1..32 characters", label)
	case c.GetChannel() == 0:
		return fmt.Errorf("%s: channel is required", label)
	default:
		return nil
	}
}
