package mgmt

import "github.com/mdlayher/wifi"

// WirelessProvider is the interface that wraps the wireless hardware operations
// used by the API handlers. *WirelessConfig satisfies this interface.
type WirelessProvider interface {
	Interfaces() ([]*wifi.Interface, error)
	GetMeshInterfaces() ([]*wifi.Interface, error)
	StationInfo(iface *wifi.Interface) ([]*wifi.StationInfo, error)
}
