package config

import (
	"fmt"
	"maps"
)

const (
	// ATAKSAAddress is the default multicast address for ATAK Situational Awareness
	ATAKSAAddress string = "239.2.3.1"

	// ATAKChatAddress is the default multicast address for ATAK Chat
	ATAKChatAddress string = "224.10.10.1"

	// MDNSAddress is the multicast address used for mDNS (Multicast DNS) service discovery
	MDNSAddress string = "224.0.0.251"

	// TalkGroupMcastAddr is the single multicast address shared by all talk groups.
	// Individual talk groups are distinguished by port number, which avoids
	// IGMP leave/join overhead when switching channels at runtime.
	TalkGroupMcastAddr string = "239.192.41.1"

	// 38801-38864 are reserved for talk groups; channels 1-32 map to ports
	// in this range. The default talk group (channel 1) uses port 38801.
	DefaultTalkGroupPort int = 38801
)

var (
	// multicastGroupAddresses is a list of multicast addresses used for various services
	multicastGroupAddresses = []string{ //nolint:gochecknoglobals
		ATAKSAAddress,
		ATAKChatAddress,
		MDNSAddress,
		TalkGroupMcastAddr,
	}

	// multicastGroupSet is a set of multicast addresses for quick lookup
	multicastGroupSet = map[string]bool{ //nolint:gochecknoglobals
		ATAKSAAddress:      true,
		ATAKChatAddress:    true,
		MDNSAddress:        true,
		TalkGroupMcastAddr: true,
	}

	// multicastTalkGroupPorts lists the UDP ports for each talk group.
	// All talk groups share TalkGroupMcastAddr; switching groups only
	// requires rebinding to a different port (no IGMP leave/join).
	// Ports are in the range 38801-38864
	multicastTalkGroupPorts = []int{ //nolint:gochecknoglobals
		38801,
		38803,
		38805,
		38807,
		38809,
	}
)

// GetMulticastGroupAddresses returns a combined slice of all multicast group addresses,
// including both the standard multicast group addresses and the talk group address.
// The returned slice is a new copy, so modifications to it will not affect the original slices.
func GetMulticastGroupAddresses() []string {
	result := make([]string, len(multicastGroupAddresses))
	copy(result, multicastGroupAddresses)

	return result
}

// TalkGroup pairs a multicast address with a UDP port.
type TalkGroup struct {
	Address string
	Port    int
}

// GetMulticastTalkGroups returns the configured talk groups. Each entry
// shares TalkGroupMcastAddr and differs only by port.
func GetMulticastTalkGroups() []TalkGroup {
	groups := make([]TalkGroup, len(multicastTalkGroupPorts))
	for i, port := range multicastTalkGroupPorts {
		groups[i] = TalkGroup{Address: TalkGroupMcastAddr, Port: port}
	}

	return groups
}

// GetMulticastTalkGroupAddresses returns a slice containing the single
// talk-group multicast address repeated once per configured talk group.
// Retained for backward compatibility with callers that index by position.
func GetMulticastTalkGroupAddresses() []string {
	result := make([]string, len(multicastTalkGroupPorts))
	for i := range result {
		result[i] = TalkGroupMcastAddr
	}

	return result
}

// talkGroupBasePort is the base for computing talk-group ports.
// Channel n (1-based) maps to talkGroupBasePort + (n-1) * talkGroupPortStride.
const (
	talkGroupPortStride = 2
	talkGroupMaxChannel = 32
)

// TalkGroupPort maps a 1-based channel number to its UDP port.
// Channel 1 → 38801, channel 2 → 38803, etc.
// Returns an error if channel is out of the valid range [1, 32].
func TalkGroupPort(channel int) (int, error) {
	if channel < 1 || channel > talkGroupMaxChannel {
		return 0, fmt.Errorf("talk group channel %d out of range [1, %d]", channel, talkGroupMaxChannel)
	}

	return DefaultTalkGroupPort + (channel-1)*talkGroupPortStride, nil
}

// GetMulticastGroupSet returns a set of all multicast group addresses as a map
// where keys are address strings and values are booleans set to true.
// The returned map is a copy and can be safely modified without affecting the
// original data.
func GetMulticastGroupSet() map[string]bool {
	result := make(map[string]bool)
	maps.Copy(result, multicastGroupSet)

	return result
}
