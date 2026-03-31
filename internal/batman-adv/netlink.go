package batmanadv

// batman-adv generic netlink constants.
// Sourced from Linux kernel include/uapi/linux/batman_adv.h (kernel 6.1+).

const (
	// BatadvNLName is the generic netlink family name for batman-adv.
	BatadvNLName = "batadv"

	// BatadvNLMcgrpConfig is the multicast group for config change notifications.
	BatadvNLMcgrpConfig = "config"

	// BatadvNLMcgrpTpmeter is the multicast group for throughput meter results.
	BatadvNLMcgrpTpmeter = "tpmeter"
)

// batman-adv generic netlink commands (BATADV_CMD_*).
const (
	BatadvCmdUnspec          uint8 = 0
	BatadvCmdGetMesh         uint8 = 1
	BatadvCmdGetMeshInfo     uint8 = 1 // alias
	BatadvCmdSetMesh         uint8 = 2
	BatadvCmdGetHardif       uint8 = 3
	BatadvCmdGetHardifInfo   uint8 = 3 // alias
	BatadvCmdSetHardif       uint8 = 4
	BatadvCmdGetVlan         uint8 = 5
	BatadvCmdSetVlan         uint8 = 6
	BatadvCmdGetNeighbors    uint8 = 7
	BatadvCmdGetOriginators  uint8 = 8
	BatadvCmdGetGateways     uint8 = 9
	BatadvCmdGetBLAClaims    uint8 = 10
	BatadvCmdGetBLABackbones uint8 = 11
	BatadvCmdGetDATCache     uint8 = 12
	BatadvCmdGetMcastFlags   uint8 = 13
	BatadvCmdGetTpMeter      uint8 = 14
	BatadvCmdCancelTpMeter   uint8 = 15
)

// batman-adv netlink attributes (BATADV_ATTR_*).
// Attribute IDs for messages to/from the batman-adv genl family.
const (
	BatadvAttrUnspec                    uint16 = 0
	BatadvAttrVersion                   uint16 = 1
	BatadvAttrAlgoName                  uint16 = 2
	BatadvAttrMeshIfindex               uint16 = 3
	BatadvAttrMeshIfname                uint16 = 4
	BatadvAttrMeshAddress               uint16 = 5
	BatadvAttrHardIfindex               uint16 = 6
	BatadvAttrHardIfname                uint16 = 7
	BatadvAttrHardAddress               uint16 = 8
	BatadvAttrOrigAddress               uint16 = 9
	BatadvAttrTQ                        uint16 = 10
	BatadvAttrThroughput                uint16 = 11
	BatadvAttrBandwidthUp               uint16 = 12
	BatadvAttrBandwidthDown             uint16 = 13
	BatadvAttrRouter                    uint16 = 14
	BatadvAttrBLAOwn                    uint16 = 15
	BatadvAttrBLAAddress                uint16 = 16
	BatadvAttrBLAVID                    uint16 = 17
	BatadvAttrBLABackboneAddr           uint16 = 18
	BatadvAttrBLACRC                    uint16 = 19
	BatadvAttrDATCacheIP4Address        uint16 = 20
	BatadvAttrDATCacheMacAddress        uint16 = 21
	BatadvAttrDATCacheVID               uint16 = 22
	BatadvAttrMcastFlags                uint16 = 23
	BatadvAttrMcastFlagsPriv            uint16 = 24
	BatadvAttrNeighAddress              uint16 = 25
	BatadvAttrTpMeterNet                uint16 = 26
	BatadvAttrTpMeterResult             uint16 = 27
	BatadvAttrTpMeterTestTime           uint16 = 28
	BatadvAttrTpMeterBytes              uint16 = 29
	BatadvAttrTpMeterCookie             uint16 = 30
	BatadvAttrActive                    uint16 = 31
	BatadvAttrTTAddress                 uint16 = 32
	BatadvAttrTTTTVN                    uint16 = 33
	BatadvAttrTTLastTTVN                uint16 = 34
	BatadvAttrTTCRC32                   uint16 = 35
	BatadvAttrTTVID                     uint16 = 36
	BatadvAttrTTFlags                   uint16 = 37
	BatadvAttrFlagBest                  uint16 = 38
	BatadvAttrLastSeenMsecs             uint16 = 39
	BatadvAttrNeighIfindex              uint16 = 40
	BatadvAttrNeighIfname               uint16 = 41
	BatadvAttrNeighAddress6             uint16 = 42
	BatadvAttrNeighName                 uint16 = 43
	BatadvAttrIsolationMark             uint16 = 44
	BatadvAttrIsolationMask             uint16 = 45
	BatadvAttrAggregatedOgmsEnabled     uint16 = 46
	BatadvAttrAPisolationEnabled        uint16 = 47
	BatadvAttrBondingEnabled            uint16 = 48
	BatadvAttrBridgeLoopAvoidEnabled    uint16 = 49
	BatadvAttrDATEnabled                uint16 = 50
	BatadvAttrFragEnabled               uint16 = 51
	BatadvAttrGwBandwidthDown           uint16 = 52
	BatadvAttrGwBandwidthUp             uint16 = 53
	BatadvAttrGwMode                    uint16 = 54
	BatadvAttrGwSelClass                uint16 = 55
	BatadvAttrHopPenalty                uint16 = 56
	BatadvAttrLogLevel                  uint16 = 57
	BatadvAttrMulticastForcefloodEnable uint16 = 58
	BatadvAttrNetworkCoding             uint16 = 59
	BatadvAttrOrigInterval              uint16 = 60
	BatadvAttrELP                       uint16 = 61
	BatadvAttrAPisolationVlan           uint16 = 62
	BatadvAttrVlanID                    uint16 = 63
	BatadvAttrMulticastFanout           uint16 = 64
)

// batman-adv gateway mode values (BATADV_GW_MODE_*).
const (
	BatadvGwModeOff    uint8 = 0
	BatadvGwModeClient uint8 = 1
	BatadvGwModeServer uint8 = 2
)

// gwModeStrings maps gateway mode integer values to their string representations.
var gwModeStrings = map[uint8]string{ //nolint:gochecknoglobals // constant lookup table
	BatadvGwModeOff:    "off",
	BatadvGwModeClient: "client",
	BatadvGwModeServer: "server",
}

// batman-adv multicast flags bitmask values (BATADV_MCAST_WANT_*).
const (
	BatadvMcastWantAllUnsnoopables uint32 = 1 << 0
	BatadvMcastWantAllIPv4         uint32 = 1 << 1
	BatadvMcastWantAllIPv6         uint32 = 1 << 2
	BatadvMcastWantNoRtrIPv4       uint32 = 1 << 3
	BatadvMcastWantNoRtrIPv6       uint32 = 1 << 4
)

// batman-adv multicast flags priv bitmask values.
const (
	BatadvMcastFlagsPrivBridged              uint32 = 1 << 0
	BatadvMcastFlagsPrivQuerierIPv4Exists    uint32 = 1 << 1
	BatadvMcastFlagsPrivQuerierIPv6Exists    uint32 = 1 << 2
	BatadvMcastFlagsPrivQuerierIPv4Shadowing uint32 = 1 << 3
	BatadvMcastFlagsPrivQuerierIPv6Shadowing uint32 = 1 << 4
)
