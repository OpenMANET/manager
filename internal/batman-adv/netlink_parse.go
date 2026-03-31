package batmanadv

import (
	"fmt"
	"net"
	"strings"

	"github.com/mdlayher/netlink"
)

// parseMeshConfig converts netlink attributes from a BATADV_CMD_GET_MESH
// response into a MeshConfig struct.
func parseMeshConfig(attrs []netlink.Attribute) (*MeshConfig, error) { //nolint:gocyclo // flat switch over many attribute types
	cfg := &MeshConfig{}

	for _, a := range attrs {
		switch a.Type {
		case BatadvAttrVersion:
			cfg.Version = trimNull(string(a.Data))
		case BatadvAttrAlgoName:
			cfg.AlgoName = trimNull(string(a.Data))
		case BatadvAttrMeshIfindex:
			cfg.MeshIfindex = int(nlUint32(a.Data))
		case BatadvAttrMeshIfname:
			cfg.MeshIfname = trimNull(string(a.Data))
		case BatadvAttrMeshAddress:
			cfg.MeshAddress = formatMAC(a.Data)
		case BatadvAttrHardIfindex:
			cfg.HardIfindex = int(nlUint32(a.Data))
		case BatadvAttrHardIfname:
			cfg.HardIfname = trimNull(string(a.Data))
		case BatadvAttrHardAddress:
			cfg.HardAddress = formatMAC(a.Data)
		case BatadvAttrTTTTVN:
			cfg.TtTtvn = int(nlUint32(a.Data))
		case BatadvAttrBLACRC:
			cfg.BlaCrc = int(nlUint32(a.Data))
		case BatadvAttrIsolationMark:
			cfg.IsolationMark = int(nlUint32(a.Data))
		case BatadvAttrIsolationMask:
			cfg.IsolationMask = int(nlUint32(a.Data))
		case BatadvAttrGwBandwidthDown:
			cfg.GwBandwidthDown = int(nlUint32(a.Data))
		case BatadvAttrGwBandwidthUp:
			cfg.GwBandwidthUp = int(nlUint32(a.Data))
		case BatadvAttrGwMode:
			mode := nlUint8(a.Data)
			if s, ok := gwModeStrings[mode]; ok {
				cfg.GwMode = s
			} else {
				cfg.GwMode = fmt.Sprintf("unknown(%d)", mode)
			}
		case BatadvAttrGwSelClass:
			cfg.GwSelClass = int(nlUint32(a.Data))
		case BatadvAttrHopPenalty:
			cfg.HopPenalty = int(nlUint32(a.Data))
		case BatadvAttrOrigInterval:
			cfg.OrigInterval = int(nlUint32(a.Data))
		case BatadvAttrMulticastFanout:
			cfg.MulticastFanout = int(nlUint32(a.Data))
		case BatadvAttrAggregatedOgmsEnabled:
			cfg.AggregatedOgmsEnabled = nlBool(a.Data)
		case BatadvAttrAPisolationEnabled:
			cfg.ApIsolationEnabled = nlBool(a.Data)
		case BatadvAttrBondingEnabled:
			cfg.BondingEnabled = nlBool(a.Data)
		case BatadvAttrBridgeLoopAvoidEnabled:
			cfg.BridgeLoopAvoidanceEnabled = nlBool(a.Data)
		case BatadvAttrDATEnabled:
			cfg.DistributedArpTableEnabled = nlBool(a.Data)
		case BatadvAttrFragEnabled:
			cfg.FragmentationEnabled = nlBool(a.Data)
		case BatadvAttrMulticastForcefloodEnable:
			cfg.MulticastForcefloodEnabled = nlBool(a.Data)
		case BatadvAttrMcastFlags:
			raw := nlUint32(a.Data)
			cfg.McastFlags = decodeMcastFlags(raw)
		case BatadvAttrMcastFlagsPriv:
			raw := nlUint32(a.Data)
			cfg.McastFlagsPriv = decodeMcastFlagsPriv(raw)
		}
	}

	return cfg, nil
}

// parseGateway converts netlink attributes from a single gateway entry
// in a BATADV_CMD_GET_GATEWAYS dump response into a Gateway struct.
func parseGateway(attrs []netlink.Attribute) (*Gateway, error) { //nolint:unparam // error reserved for future validation
	gw := &Gateway{}

	for _, a := range attrs {
		switch a.Type {
		case BatadvAttrHardIfindex:
			gw.HardIfindex = int(nlUint32(a.Data))
		case BatadvAttrHardIfname:
			gw.HardIfname = trimNull(string(a.Data))
		case BatadvAttrOrigAddress:
			gw.OrigAddress = formatMAC(a.Data)
		case BatadvAttrRouter:
			gw.Router = formatMAC(a.Data)
		case BatadvAttrThroughput:
			gw.Throughput = int(nlUint32(a.Data))
		case BatadvAttrBandwidthUp:
			gw.BandwidthUp = int(nlUint32(a.Data))
		case BatadvAttrBandwidthDown:
			gw.BandwidthDown = int(nlUint32(a.Data))
		case BatadvAttrFlagBest:
			gw.Best = nlBool(a.Data)
		}
	}

	return gw, nil
}

// parseNeighbor converts netlink attributes from a single neighbor entry
// in a BATADV_CMD_GET_NEIGHBORS dump response into a Neighbor struct.
func parseNeighbor(attrs []netlink.Attribute) (*Neighbor, error) { //nolint:unparam // error reserved for future validation
	n := &Neighbor{}

	for _, a := range attrs {
		switch a.Type {
		case BatadvAttrHardIfindex:
			n.HardIfindex = int(nlUint32(a.Data))
		case BatadvAttrHardIfname:
			n.HardIfname = trimNull(string(a.Data))
		case BatadvAttrNeighAddress:
			n.NeighAddress = formatMAC(a.Data)
		case BatadvAttrLastSeenMsecs:
			n.LastSeenMsecs = int(nlUint32(a.Data))
		case BatadvAttrThroughput:
			n.Throughput = int(nlUint32(a.Data))
		}
	}

	return n, nil
}

// parseOriginator converts netlink attributes from a single originator entry
// in a BATADV_CMD_GET_ORIGINATORS dump response into an Originator struct.
func parseOriginator(attrs []netlink.Attribute) (*Originator, error) { //nolint:unparam // error reserved for future validation
	o := &Originator{}

	for _, a := range attrs {
		switch a.Type {
		case BatadvAttrOrigAddress:
			o.OrigAddress = formatMAC(a.Data)
		case BatadvAttrHardIfname:
			o.HardIfname = trimNull(string(a.Data))
		case BatadvAttrNeighAddress:
			o.BestNeigh = formatMAC(a.Data)
		case BatadvAttrLastSeenMsecs:
			o.LastSeenMs = int(nlUint32(a.Data))
		case BatadvAttrTQ:
			o.TQ = int(nlUint8(a.Data))
		}
	}

	return o, nil
}

// decodeMcastFlags decomposes a raw multicast flags bitmask into the McastFlags struct.
func decodeMcastFlags(raw uint32) McastFlags {
	return McastFlags{
		AllUnsnoopables: raw&BatadvMcastWantAllUnsnoopables != 0,
		WantAllIpv4:     raw&BatadvMcastWantAllIPv4 != 0,
		WantAllIpv6:     raw&BatadvMcastWantAllIPv6 != 0,
		WantNoRtrIpv4:   raw&BatadvMcastWantNoRtrIPv4 != 0,
		WantNoRtrIpv6:   raw&BatadvMcastWantNoRtrIPv6 != 0,
		Raw:             int(raw),
	}
}

// decodeMcastFlagsPriv decomposes a raw private multicast flags bitmask.
func decodeMcastFlagsPriv(raw uint32) McastFlagsPriv {
	return McastFlagsPriv{
		Bridged:              raw&BatadvMcastFlagsPrivBridged != 0,
		QuerierIpv4Exists:    raw&BatadvMcastFlagsPrivQuerierIPv4Exists != 0,
		QuerierIpv6Exists:    raw&BatadvMcastFlagsPrivQuerierIPv6Exists != 0,
		QuerierIpv4Shadowing: raw&BatadvMcastFlagsPrivQuerierIPv4Shadowing != 0,
		QuerierIpv6Shadowing: raw&BatadvMcastFlagsPrivQuerierIPv6Shadowing != 0,
		Raw:                  int(raw),
	}
}

// formatMAC formats a 6-byte binary MAC address as a lowercase colon-separated string.
func formatMAC(data []byte) string {
	if len(data) < 6 {
		return ""
	}

	return net.HardwareAddr(data[:6]).String()
}

// trimNull removes trailing null bytes from a string.
func trimNull(s string) string {
	return strings.TrimRight(s, "\x00")
}

// nlUint32 reads a little-endian uint32 from a netlink attribute data slice.
func nlUint32(data []byte) uint32 {
	if len(data) < 4 {
		return 0
	}

	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

// nlUint8 reads a single byte from a netlink attribute data slice.
func nlUint8(data []byte) uint8 {
	if len(data) < 1 {
		return 0
	}

	return data[0]
}

// nlBool interprets a netlink attribute as a boolean (non-zero = true).
func nlBool(data []byte) bool {
	return nlUint8(data) != 0
}
