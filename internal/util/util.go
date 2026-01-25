package util

import (
	"fmt"
	"strings"
)

// InterfaceWithoutBridge removes the "br-" prefix from a bridge interface name.
// It returns the underlying physical interface name and nil error if the interface
// is a bridge (prefixed with "br-"). If the interface is not a bridge interface,
// it returns the original interface name and an error indicating that the interface
// is not a bridge.
//
// Parameters:
//   - iface: The interface name to process
//
// Returns:
//   - string: The interface name without the "br-" prefix
//   - error: An error if the interface is not a bridge interface
func InterfaceWithoutBridge(iface string) (string, error) {
	//InterFace is prefixed with "br-", remove the prefix because dhcp and network config is tied to the physical interface
	if after, ok := strings.CutPrefix(iface, "br-"); ok {
		return after, nil
	}

	return iface, fmt.Errorf("interface %s is not a bridge interface", iface)
}
