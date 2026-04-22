package handlers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

// blosRateWindow is the look-back window the handler uses when computing
// BLOSCounters rates. Matches the rolling-ring window in the status worker.
const blosRateWindow = 60 * time.Second

// blosEventChanSize is the buffered-channel capacity used between the BLOS
// event listener and the StreamBLOSEvents server stream. Large enough to
// absorb a burst of peer events without dropping under normal conditions;
// small enough that a truly stuck client is detected quickly.
const blosEventChanSize = 64

// tailscaleKeepaliveInterval is the Tailscale peer keepalive cadence
// reported in BLOSDERP responses. The Tailscale daemon uses a 25-second
// keepalive and openmanetd does not override it.
const tailscaleKeepaliveInterval = 25 * time.Second

// BLOSService implements the BLOS ConnectRPC service.
type BLOSService struct {
	Cfg         *config.Config
	Log         zerolog.Logger
	BLOSManager blos.BLOSLifecycle
}

// GetBLOSStatus retrieves the current status of the BLOS subsystem.
func (b *BLOSService) GetBLOSStatus(ctx context.Context, _ *emptypb.Empty) (*v1.GetBLOSStatusResponse, error) {
	enabled := b.Cfg.BLOSEnabled()
	running := b.BLOSManager.IsRunning()

	var message string

	switch {
	case enabled && running:
		message = "BLOS subsystem is enabled and running."
	case enabled && !running:
		message = "BLOS subsystem is enabled in config but not currently running."
	default:
		message = "BLOS subsystem is disabled."
	}

	resp := &v1.GetBLOSStatusResponse{
		BlosEnabled: enabled,
		Message:     &message,
	}

	if !running {
		return resp, nil
	}

	status := b.BLOSManager.Status()
	if status == nil {
		return resp, nil
	}

	resp.Identity = buildIdentity(status, b.BLOSManager.ConnectedSince())
	resp.Derp = buildSelfDERP(status)
	resp.Counters = b.buildCounters()

	prefs, err := b.BLOSManager.Prefs(ctx)
	if err != nil {
		b.Log.Warn().Err(err).Msg("Failed to fetch Tailscale prefs for BLOS status")
	}

	resp.Network = buildNetworkConfig(status, prefs)

	return resp, nil
}

// ListBLOSPeers returns the remote peers visible on the Tailscale overlay.
func (b *BLOSService) ListBLOSPeers(_ context.Context, _ *emptypb.Empty) (*v1.ListBLOSPeersResponse, error) {
	resp := &v1.ListBLOSPeersResponse{}

	if !b.BLOSManager.IsRunning() {
		return resp, nil
	}

	status := b.BLOSManager.Status()
	if status == nil {
		return resp, nil
	}

	resp.Peers = make([]*v1.BLOSPeer, 0, len(status.Peer))

	for _, p := range status.Peer {
		if p == nil {
			continue
		}

		resp.Peers = append(resp.Peers, peerToProto(p))
	}

	sort.Slice(resp.Peers, func(i, j int) bool {
		return resp.Peers[i].Hostname < resp.Peers[j].Hostname
	})

	return resp, nil
}

// StreamBLOSEvents streams BLOS state-change events to the client.
// Each event is wrapped in a StreamBLOSEventsResponse envelope so the
// response type follows the buf naming convention.
func (b *BLOSService) StreamBLOSEvents(ctx context.Context, _ *emptypb.Empty, stream *connect.ServerStream[v1.StreamBLOSEventsResponse]) error {
	if !b.BLOSManager.IsRunning() {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("BLOS subsystem is not running"))
	}

	ch := make(chan *v1.BLOSEvent, blosEventChanSize)

	id := b.BLOSManager.AddEventListener(func(ev blos.Event) {
		proto := eventToProto(ev)
		select {
		case ch <- proto:
		default:
			b.BLOSManager.NoteEventDropped()
		}
	})
	if id == 0 {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("BLOS subsystem is not running"))
	}

	defer b.BLOSManager.RemoveEventListener(id)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}

			if err := stream.Send(&v1.StreamBLOSEventsResponse{Event: ev}); err != nil {
				return err
			}
		}
	}
}

// UpdateBLOSConfig updates the BLOS subsystem configuration. When enabling,
// it first authenticates with Tailscale via the SDK, then persists the config
// change, and finally starts the BLOS module.
func (b *BLOSService) UpdateBLOSConfig(ctx context.Context, req *v1.UpdateBLOSConfigRequest) (*v1.UpdateBLOSConfigResponse, error) {
	if req.EnableBlos {
		return b.enableBLOS(ctx, req)
	}

	return b.disableBLOS()
}

func (b *BLOSService) enableBLOS(ctx context.Context, req *v1.UpdateBLOSConfigRequest) (*v1.UpdateBLOSConfigResponse, error) {
	if strings.TrimSpace(req.AuthKey) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("auth_key is required"))
	}

	var loginServerURL string
	if req.LoginServerUrl != nil && *req.LoginServerUrl != "" {
		loginServerURL = *req.LoginServerUrl
	}

	b.Log.Info().Msg("Configuring Tailscale and enabling BLOS")

	if err := b.BLOSManager.ConfigureAndEnable(ctx, req.AuthKey, loginServerURL); err != nil {
		b.Log.Error().Err(err).Msg("Failed to configure and enable BLOS")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to enable BLOS: %w", err))
	}

	message := "BLOS enabled successfully."

	return &v1.UpdateBLOSConfigResponse{
		Success: true,
		Message: &message,
	}, nil
}

func (b *BLOSService) disableBLOS() (*v1.UpdateBLOSConfigResponse, error) {
	b.BLOSManager.Disable()

	if err := b.Cfg.PersistBLOSConfig(false); err != nil {
		b.Log.Error().Err(err).Msg("Failed to persist BLOS config, re-enabling")

		if reErr := b.BLOSManager.Enable(context.Background()); reErr != nil {
			b.Log.Error().Err(reErr).Msg("Failed to re-enable BLOS during rollback")
		}

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to disable BLOS: config persistence failed, rolled back: %w", err))
	}

	message := "BLOS disabled successfully."

	return &v1.UpdateBLOSConfigResponse{
		Success: true,
		Message: &message,
	}, nil
}

// buildCounters is a method rather than a free function so the handler's
// lifecycle interface is the sole seam the rate window goes through.
func (b *BLOSService) buildCounters() *v1.BLOSCounters {
	rxBps, txBps, rxTotal, txTotal := b.BLOSManager.RateWindow(blosRateWindow)

	return &v1.BLOSCounters{
		RxBytesTotal:  rxTotal,
		TxBytesTotal:  txTotal,
		RxBytesPerSec: rxBps,
		TxBytesPerSec: txBps,
	}
}

// buildIdentity translates the self-identifying fields of an ipnstate.Status
// into the proto representation.
func buildIdentity(status *ipnstate.Status, connectedSince time.Time) *v1.BLOSTunnelIdentity {
	id := &v1.BLOSTunnelIdentity{
		BackendState: status.BackendState,
	}

	if status.Self != nil {
		id.Hostname = status.Self.HostName
		id.DnsName = status.Self.DNSName
		id.Os = status.Self.OS
		id.OverlayIps = make([]string, 0, len(status.Self.TailscaleIPs))

		for _, ip := range status.Self.TailscaleIPs {
			id.OverlayIps = append(id.OverlayIps, ip.String())
		}
	}

	if !connectedSince.IsZero() {
		id.ConnectedSince = timestamppb.New(connectedSince)
	}

	if status.CurrentTailnet != nil {
		id.MagicDnsSuffix = status.CurrentTailnet.MagicDNSSuffix
		id.TailnetName = status.CurrentTailnet.Name
	}

	return id
}

// buildSelfDERP translates the local node's DERP state into proto form.
func buildSelfDERP(status *ipnstate.Status) *v1.BLOSDERP {
	d := &v1.BLOSDERP{
		KeepaliveInterval: durationpb.New(tailscaleKeepaliveInterval),
	}

	if status.Self == nil {
		return d
	}

	d.Region = status.Self.Relay
	d.Endpoint = status.Self.CurAddr

	switch {
	case status.BackendState != "Running":
		d.Path = ""
	case status.Self.CurAddr != "":
		d.Path = "direct"
	default:
		d.Path = "derp"
	}

	return d
}

// buildNetworkConfig extracts the overlay-network policy (exit node,
// advertised routes, ACL tags) from the live Tailscale state.
func buildNetworkConfig(status *ipnstate.Status, prefs *ipn.Prefs) *v1.BLOSNetworkConfig {
	cfg := &v1.BLOSNetworkConfig{}

	if status != nil && status.Self != nil && status.Self.Tags != nil {
		tags := status.Self.Tags
		cfg.AclTags = make([]string, 0, tags.Len())

		for i := range tags.Len() {
			cfg.AclTags = append(cfg.AclTags, tags.At(i))
		}
	}

	if prefs == nil {
		return cfg
	}

	cfg.ExitNode = prefs.ExitNodeIP.IsValid()
	cfg.AdvertisedRoutes = make([]string, 0, len(prefs.AdvertiseRoutes))

	for _, r := range prefs.AdvertiseRoutes {
		cfg.AdvertisedRoutes = append(cfg.AdvertisedRoutes, r.String())
	}

	return cfg
}

// peerToProto translates an ipnstate.PeerStatus into the wire representation.
func peerToProto(p *ipnstate.PeerStatus) *v1.BLOSPeer {
	out := &v1.BLOSPeer{
		Hostname:    p.HostName,
		DnsName:     p.DNSName,
		Os:          p.OS,
		Endpoint:    p.CurAddr,
		RelayRegion: p.Relay,
		Online:      p.Online,
		Active:      p.Active,
		RxBytes:     uint64(p.RxBytes),
		TxBytes:     uint64(p.TxBytes),
	}

	out.OverlayIps = make([]string, 0, len(p.TailscaleIPs))
	for _, ip := range p.TailscaleIPs {
		out.OverlayIps = append(out.OverlayIps, ip.String())
	}

	if !p.LastSeen.IsZero() {
		out.LastSeen = timestamppb.New(p.LastSeen)
	}

	if !p.LastHandshake.IsZero() {
		out.LastHandshake = timestamppb.New(p.LastHandshake)
	}

	if p.Tags != nil {
		out.Tags = make([]string, 0, p.Tags.Len())

		for i := range p.Tags.Len() {
			out.Tags = append(out.Tags, p.Tags.At(i))
		}
	}

	out.PrimaryRoutes = primaryRoutesToStrings(p)

	switch {
	case p.CurAddr != "":
		out.Path = "direct"
	case p.Relay != "":
		out.Path = "derp"
	default:
		out.Path = ""
	}

	return out
}

// primaryRoutesToStrings extracts PrimaryRoutes from a PeerStatus as CIDR
// strings. Defends against nil views without allocating a full slice when
// the peer advertises no routes.
func primaryRoutesToStrings(p *ipnstate.PeerStatus) []string {
	if p.PrimaryRoutes == nil {
		return nil
	}

	n := p.PrimaryRoutes.Len()
	if n == 0 {
		return nil
	}

	out := make([]string, 0, n)

	for i := range n {
		out = append(out, p.PrimaryRoutes.At(i).String())
	}

	return out
}

// eventToProto translates a blos.Event into the wire form.
func eventToProto(ev blos.Event) *v1.BLOSEvent {
	return &v1.BLOSEvent{
		Ts:      timestamppb.New(ev.At),
		Kind:    v1.BLOSEventKind(ev.Kind),
		Subject: ev.Subject,
		Message: ev.Message,
	}
}
