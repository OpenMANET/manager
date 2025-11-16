package batmanadv

import (
	"encoding/json"
	"os/exec"
	"sort"
)

type Gateway struct {
	HardIfindex   int    `json:"hard_ifindex"`
	HardIfname    string `json:"hard_ifname"`
	OrigAddress   string `json:"orig_address"`
	Best          bool   `json:"best"`
	Throughput    int    `json:"throughput"`
	BandwidthUp   int    `json:"bandwidth_up"`
	BandwidthDown int    `json:"bandwidth_down"`
	Router        string `json:"router"`
}

type Gateways []Gateway

func GetMeshGateways(iface string) (*Gateways, error) {
	cmd := exec.Command("batctl", "gwj")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var gateways Gateways
	err = json.Unmarshal(output, &gateways)
	if err != nil {
		return nil, err
	}

	return &gateways, nil
}

// GetBest returns the best gateway from the list, or nil if none is marked as best
func (gws *Gateways) GetBest() *Gateway {
	if gws == nil {
		return nil
	}
	for i := range *gws {
		if (*gws)[i].Best {
			return &(*gws)[i]
		}
	}
	return nil
}

// FindByOrigAddress returns the gateway with the specified originator address, or nil if not found
func (gws *Gateways) FindByOrigAddress(origAddress string) *Gateway {
	if gws == nil {
		return nil
	}
	for i := range *gws {
		if (*gws)[i].OrigAddress == origAddress {
			return &(*gws)[i]
		}
	}
	return nil
}

// FindByInterface returns the gateway on the specified interface, or nil if not found
func (gws *Gateways) FindByInterface(ifname string) *Gateway {
	if gws == nil {
		return nil
	}
	for i := range *gws {
		if (*gws)[i].HardIfname == ifname {
			return &(*gws)[i]
		}
	}
	return nil
}

// FilterByInterface returns all gateways on the specified interface
func (gws *Gateways) FilterByInterface(ifname string) Gateways {
	if gws == nil {
		return Gateways{}
	}
	var filtered Gateways
	for _, gw := range *gws {
		if gw.HardIfname == ifname {
			filtered = append(filtered, gw)
		}
	}
	return filtered
}

// Count returns the number of gateways in the list
func (gws *Gateways) Count() int {
	if gws == nil {
		return 0
	}
	return len(*gws)
}

// IsEmpty returns true if there are no gateways in the list
func (gws *Gateways) IsEmpty() bool {
	return gws == nil || len(*gws) == 0
}

// HasBest returns true if any gateway is marked as best
func (gws *Gateways) HasBest() bool {
	return gws.GetBest() != nil
}

// GetHighestThroughput returns the gateway with the highest throughput
func (gws *Gateways) GetHighestThroughput() *Gateway {
	if gws == nil || len(*gws) == 0 {
		return nil
	}
	var best *Gateway
	maxThroughput := 0
	for i := range *gws {
		if (*gws)[i].Throughput > maxThroughput {
			maxThroughput = (*gws)[i].Throughput
			best = &(*gws)[i]
		}
	}
	return best
}

// SortByThroughput sorts gateways by throughput in descending order (highest first)
func (gws *Gateways) SortByThroughput() {
	if gws == nil {
		return
	}
	sort.Slice(*gws, func(i, j int) bool {
		return (*gws)[i].Throughput > (*gws)[j].Throughput
	})
}

// SortByOrigAddress sorts gateways by originator address in ascending order
func (gws *Gateways) SortByOrigAddress() {
	if gws == nil {
		return
	}
	sort.Slice(*gws, func(i, j int) bool {
		return (*gws)[i].OrigAddress < (*gws)[j].OrigAddress
	})
}

// GetOrigAddresses returns a slice of all originator addresses
func (gws *Gateways) GetOrigAddresses() []string {
	if gws == nil {
		return []string{}
	}
	addresses := make([]string, len(*gws))
	for i, gw := range *gws {
		addresses[i] = gw.OrigAddress
	}
	return addresses
}

// GetInterfaces returns a unique list of all interfaces used by gateways
func (gws *Gateways) GetInterfaces() []string {
	if gws == nil {
		return []string{}
	}
	ifaceMap := make(map[string]bool)
	for _, gw := range *gws {
		ifaceMap[gw.HardIfname] = true
	}

	interfaces := make([]string, 0, len(ifaceMap))
	for iface := range ifaceMap {
		interfaces = append(interfaces, iface)
	}
	sort.Strings(interfaces)
	return interfaces
}

// TotalThroughput returns the sum of throughput for all gateways
func (gws *Gateways) TotalThroughput() int {
	if gws == nil {
		return 0
	}
	total := 0
	for _, gw := range *gws {
		total += gw.Throughput
	}
	return total
}

// AverageThroughput returns the average throughput across all gateways
func (gws *Gateways) AverageThroughput() float64 {
	if gws == nil || len(*gws) == 0 {
		return 0.0
	}
	return float64(gws.TotalThroughput()) / float64(len(*gws))
}

// String returns a JSON representation of the gateway list
func (gws *Gateways) String() string {
	if gws == nil {
		return "[]"
	}
	data, err := json.MarshalIndent(gws, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(data)
}
