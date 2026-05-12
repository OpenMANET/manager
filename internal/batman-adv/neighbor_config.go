package batmanadv

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
)

type Neighbor struct {
	HardIfname    string `json:"hard_ifname"`
	NeighAddress  string `json:"neigh_address"`
	HardIfindex   int    `json:"hard_ifindex"`
	LastSeenMsecs int    `json:"last_seen_msecs"`
	Throughput    int    `json:"throughput"`
}

type Neighbors []Neighbor

// GetMeshNeighbors retrieves the current mesh network neighbors from the batman-adv
// routing protocol by executing the `batctl nj` command and parsing its JSON output.
//
// It returns a pointer to a [Neighbors] struct containing the list of discovered
// mesh neighbors, or an error if the command execution fails or the output cannot
// be parsed as valid JSON.
func GetMeshNeighbors() (*Neighbors, error) {
	cmd := exec.Command("batctl", "nj") //nolint:noctx // no context needed for short-lived local command

	output, err := cmd.Output()
	if err != nil {
		return nil, err //nolint:wrapcheck // callers handle the error directly
	}

	return parseNeighbors(output)
}

// parseNeighbors unmarshals the `batctl nj` JSON payload and lower-cases
// the neighbor MAC so downstream handlers can key maps without
// per-request strings.ToLower. Exposed separately from
// GetMeshNeighbors so tests can exercise the normalization without
// exec'ing batctl.
func parseNeighbors(output []byte) (*Neighbors, error) {
	var neighbors Neighbors
	if err := json.Unmarshal(output, &neighbors); err != nil {
		return nil, err //nolint:wrapcheck // callers handle the error directly
	}

	for i := range neighbors {
		neighbors[i].NeighAddress = strings.ToLower(neighbors[i].NeighAddress)
	}

	return &neighbors, nil
}

// FindByNeighAddress returns the neighbor with the specified MAC address, or nil if not found
func (ns *Neighbors) FindByNeighAddress(mac string) *Neighbor {
	if ns == nil {
		return nil
	}

	for i := range *ns {
		if strings.EqualFold((*ns)[i].NeighAddress, mac) {
			return &(*ns)[i]
		}
	}

	return nil
}

// FilterByInterface returns all neighbors on the specified interface
func (ns *Neighbors) FilterByInterface(ifname string) Neighbors {
	if ns == nil {
		return Neighbors{}
	}

	var filtered Neighbors

	for _, n := range *ns {
		if n.HardIfname == ifname {
			filtered = append(filtered, n)
		}
	}

	return filtered
}

// Count returns the number of neighbors in the list
func (ns *Neighbors) Count() int {
	if ns == nil {
		return 0
	}

	return len(*ns)
}

// IsEmpty returns true if there are no neighbors in the list
func (ns *Neighbors) IsEmpty() bool {
	return ns == nil || len(*ns) == 0
}

// GetNeighAddresses returns a slice of all neighbor MAC addresses
func (ns *Neighbors) GetNeighAddresses() []string {
	if ns == nil {
		return []string{}
	}

	addresses := make([]string, len(*ns))
	for i, n := range *ns {
		addresses[i] = n.NeighAddress
	}

	return addresses
}

// GetInterfaces returns a unique list of all interfaces used by neighbors
func (ns *Neighbors) GetInterfaces() []string {
	if ns == nil {
		return []string{}
	}

	ifaceMap := make(map[string]bool)
	for _, n := range *ns {
		ifaceMap[n.HardIfname] = true
	}

	interfaces := make([]string, 0, len(ifaceMap))
	for iface := range ifaceMap {
		interfaces = append(interfaces, iface)
	}

	sort.Strings(interfaces)

	return interfaces
}

// GetHighestThroughput returns the neighbor with the highest throughput
func (ns *Neighbors) GetHighestThroughput() *Neighbor {
	if ns == nil || len(*ns) == 0 {
		return nil
	}

	var best *Neighbor

	maxThroughput := 0
	for i := range *ns {
		if (*ns)[i].Throughput > maxThroughput {
			maxThroughput = (*ns)[i].Throughput
			best = &(*ns)[i]
		}
	}

	return best
}

// SortByThroughput sorts neighbors by throughput in descending order (highest first)
func (ns *Neighbors) SortByThroughput() {
	if ns == nil {
		return
	}

	sort.Slice(*ns, func(i, j int) bool {
		return (*ns)[i].Throughput > (*ns)[j].Throughput
	})
}

// SortByLastSeen sorts neighbors by last seen in ascending order (most recently seen first)
func (ns *Neighbors) SortByLastSeen() {
	if ns == nil {
		return
	}

	sort.Slice(*ns, func(i, j int) bool {
		return (*ns)[i].LastSeenMsecs < (*ns)[j].LastSeenMsecs
	})
}

// String returns a JSON representation of the neighbor list
func (ns *Neighbors) String() string {
	if ns == nil {
		return "[]"
	}

	data, err := json.MarshalIndent(ns, "", "  ")
	if err != nil {
		return "[]"
	}

	return string(data)
}
