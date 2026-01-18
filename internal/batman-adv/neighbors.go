package batmanadv

import (
	"bufio"
	"os"
	"strings"
)

const BatHostsFilePath = "/tmp/bat-hosts"

// BatHost represents a single host entry with its MAC address and hostname
type BatHost struct {
	MAC      string
	Hostname string
}

// Node represents a batman-adv node with its interfaces
type Node struct {
	NodeMAC string
	Hosts   []BatHost
}

// BatHosts represents the collection of all nodes from bat-hosts file
type BatHosts struct {
	Nodes []Node
}

// ParseBatHostsFile reads and parses the /tmp/bat-hosts file
func ParseBatHostsFile(filePath string) (*BatHosts, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return ParseBatHosts(file)
}

// ParseBatHosts parses bat-hosts data from an io.Reader
func ParseBatHosts(reader *os.File) (*BatHosts, error) {
	batHosts := &BatHosts{
		Nodes: []Node{},
	}

	scanner := bufio.NewScanner(reader)
	var currentNode *Node

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and general comment lines (###)
		if line == "" || strings.HasPrefix(line, "###") {
			continue
		}

		// Check if this is a node header comment
		if strings.HasPrefix(line, "# Node ") {
			// Save previous node if exists
			if currentNode != nil {
				batHosts.Nodes = append(batHosts.Nodes, *currentNode)
			}

			// Extract node MAC address
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				nodeMAC := parts[2]
				currentNode = &Node{
					NodeMAC: nodeMAC,
					Hosts:   []BatHost{},
				}
			}
			continue
		}

		// Skip other comment lines
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Parse host entry (MAC hostname)
		fields := strings.Fields(line)
		if len(fields) >= 2 && currentNode != nil {
			host := BatHost{
				MAC:      fields[0],
				Hostname: fields[1],
			}
			currentNode.Hosts = append(currentNode.Hosts, host)
		}
	}

	// Add the last node
	if currentNode != nil {
		batHosts.Nodes = append(batHosts.Nodes, *currentNode)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return batHosts, nil
}

// GetHostByMAC returns the hostname for a given MAC address
func (bh *BatHosts) GetHostByMAC(mac string) string {
	for _, node := range bh.Nodes {
		for _, host := range node.Hosts {
			if strings.EqualFold(host.MAC, mac) {
				return host.Hostname
			}
		}
	}
	return ""
}

// GetNodeByMAC returns the node for a given node MAC address
func (bh *BatHosts) GetNodeByMAC(nodeMAC string) *Node {
	for _, node := range bh.Nodes {
		if strings.EqualFold(node.NodeMAC, nodeMAC) {
			return &node
		}
	}
	return nil
}
