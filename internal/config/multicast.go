package config

import "maps"

const (
	// ATAKSAAddress is the default multicast address for ATAK Situational Awareness
	ATAKSAAddress string = "239.2.3.1"

	// ATAKChatAddress is the default multicast address for ATAK Chat
	ATAKChatAddress string = "224.10.10.1"

	// MDNSAddress is the multicast address used for mDNS (Multicast DNS) service discovery
	MDNSAddress string = "224.0.0.251"
)

var (
	// multicastGroupAddresses is a list of multicast addresses used for various services
	multicastGroupAddresses = []string{
		ATAKSAAddress,
		ATAKChatAddress,
		MDNSAddress,
	}

	// multicastGroupSet is a set of multicast addresses for quick lookup
	multicastGroupSet = map[string]bool{
		ATAKSAAddress:   true,
		ATAKChatAddress: true,
		MDNSAddress:     true,
	}

	// multicastTalkGroupAddresses is a list of multicast addresses used for talk groups
	multicastTalkGroupAddresses = []string{
		"225.41.1.1",
		"225.41.1.2",
		"225.41.1.3",
		"225.41.1.4",
		"225.41.1.5",
	}
)

// GetMulticastGroupAddresses returns a combined slice of all multicast group addresses,
// including both the standard multicast group addresses and the multicast talk group addresses.
// The returned slice is a new copy, so modifications to it will not affect the original slices.
func GetMulticastGroupAddresses() []string {
	result := make([]string, len(multicastGroupAddresses)+len(multicastTalkGroupAddresses))
	copy(result, multicastGroupAddresses)
	copy(result[len(multicastGroupAddresses):], multicastTalkGroupAddresses)

	return result
}

// GetMulticastGroupSet returns a set of all multicast group addresses as a map
// where keys are address strings and values are booleans set to true.
// The returned set is a combination of the base multicastGroupSet and any
// additional addresses from multicastTalkGroupAddresses.
// The returned map is a copy and can be safely modified without affecting the
// original data.
func GetMulticastGroupSet() map[string]bool {
	result := make(map[string]bool)
	maps.Copy(result, multicastGroupSet)

	for _, addr := range multicastTalkGroupAddresses {
		result[addr] = true
	}

	return result
}
