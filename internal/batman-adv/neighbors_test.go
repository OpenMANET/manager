package batmanadv

import (
	"os"
	"testing"
)

const testBatHostsFilePath = "../../testfixtures/batman-adv/bat-hosts"

func TestParseBatHostsFile(t *testing.T) {
	testFilePath := testBatHostsFilePath

	batHosts, err := ParseBatHostsFile(testFilePath)
	if err != nil {
		t.Fatalf("Failed to parse bat-hosts file: %v", err)
	}

	// Verify we have the expected number of nodes. The fixture carries five
	// RF nodes plus three single-entry BLOS stubs used by the originator
	// topology tests.
	expectedNodes := 8
	if len(batHosts.Nodes) != expectedNodes {
		t.Errorf("Expected %d nodes, got %d", expectedNodes, len(batHosts.Nodes))
	}

	// Test first node (0a:d7:37:78:2d:3e)
	node1 := batHosts.GetNodeByMAC("0a:d7:37:78:2d:3e")
	if node1 == nil {
		t.Fatal("Node 0a:d7:37:78:2d:3e not found")
	}

	expectedHostsNode1 := 5
	if len(node1.Hosts) != expectedHostsNode1 {
		t.Errorf("Node 1 expected %d hosts, got %d", expectedHostsNode1, len(node1.Hosts))
	}

	// Verify a specific host entry
	expectedMAC := "3c:22:7f:37:4c:0c"
	expectedHostname := "BCM2711-97d6_wlh0"
	found := false

	for _, host := range node1.Hosts {
		if host.MAC == expectedMAC {
			found = true

			if host.Hostname != expectedHostname {
				t.Errorf("Expected hostname %s, got %s", expectedHostname, host.Hostname)
			}

			break
		}
	}

	if !found {
		t.Errorf("Host with MAC %s not found in node 1", expectedMAC)
	}

	// Test second node (2c:cf:67:b8:88:ba)
	node2 := batHosts.GetNodeByMAC("2c:cf:67:b8:88:ba")
	if node2 == nil {
		t.Fatal("Node 2c:cf:67:b8:88:ba not found")
	}

	expectedHostsNode2 := 4
	if len(node2.Hosts) != expectedHostsNode2 {
		t.Errorf("Node 2 expected %d hosts, got %d", expectedHostsNode2, len(node2.Hosts))
	}

	// Test third node (2c:cf:67:bb:10:03)
	node3 := batHosts.GetNodeByMAC("2c:cf:67:bb:10:03")
	if node3 == nil {
		t.Fatal("Node 2c:cf:67:bb:10:03 not found")
	}

	expectedHostsNode3 := 5
	if len(node3.Hosts) != expectedHostsNode3 {
		t.Errorf("Node 3 expected %d hosts, got %d", expectedHostsNode3, len(node3.Hosts))
	}

	// Test fourth node (00:c0:ca:b6:5c:56)
	node4 := batHosts.GetNodeByMAC("00:c0:ca:b6:5c:56")
	if node4 == nil {
		t.Fatal("Node 00:c0:ca:b6:5c:56 not found")
	}

	expectedHostsNode4 := 4
	if len(node4.Hosts) != expectedHostsNode4 {
		t.Errorf("Node 4 expected %d hosts, got %d", expectedHostsNode4, len(node4.Hosts))
	}

	// Test fifth node (e4:5f:01:df:f8:fd)
	node5 := batHosts.GetNodeByMAC("e4:5f:01:df:f8:fd")
	if node5 == nil {
		t.Fatal("Node e4:5f:01:df:f8:fd not found")
	}

	expectedHostsNode5 := 5
	if len(node5.Hosts) != expectedHostsNode5 {
		t.Errorf("Node 5 expected %d hosts, got %d", expectedHostsNode5, len(node5.Hosts))
	}
}

func TestGetHostByMAC(t *testing.T) {
	testFilePath := testBatHostsFilePath

	batHosts, err := ParseBatHostsFile(testFilePath)
	if err != nil {
		t.Fatalf("Failed to parse bat-hosts file: %v", err)
	}

	tests := []struct {
		mac      string
		expected string
	}{
		{"3c:22:7f:37:4c:0c", "BCM2711-97d6_wlh0"},
		{"9c:ef:d5:f9:80:4d", "BCM2711-97d6_phy2-mesh0"},
		{"bc:2a:33:96:b1:84", "BCM2711-88ba_wlh0"},
		{"00:c0:ca:b6:5c:58", "HaLow-R-b65c57_mesh0"},
		{"e4:5f:01:df:f8:fe", "BCM2711-f8fd_wlh-f8-fe"},
		{"00:00:00:00:00:00", ""}, // Non-existent MAC
	}

	for _, tt := range tests {
		t.Run(tt.mac, func(t *testing.T) {
			hostname := batHosts.GetHostByMAC(tt.mac)
			if hostname != tt.expected {
				t.Errorf("GetHostByMAC(%s) = %s; expected %s", tt.mac, hostname, tt.expected)
			}
		})
	}
}

func TestGetHostByMAC_CaseInsensitive(t *testing.T) {
	testFilePath := testBatHostsFilePath

	batHosts, err := ParseBatHostsFile(testFilePath)
	if err != nil {
		t.Fatalf("Failed to parse bat-hosts file: %v", err)
	}

	// Test case insensitivity
	hostname1 := batHosts.GetHostByMAC("3c:22:7f:37:4c:0c")
	hostname2 := batHosts.GetHostByMAC("3C:22:7F:37:4C:0C")

	if hostname1 != hostname2 || hostname1 != "BCM2711-97d6_wlh0" {
		t.Errorf("Case insensitive lookup failed: %s vs %s", hostname1, hostname2)
	}
}

func TestParseBatHosts_EmptyFile(t *testing.T) {
	// Create a temporary empty file
	tmpFile, err := os.CreateTemp("", "bat-hosts-empty-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	defer os.Remove(tmpFile.Name())

	tmpFile.Close()

	batHosts, err := ParseBatHostsFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse empty file: %v", err)
	}

	if len(batHosts.Nodes) != 0 {
		t.Errorf("Expected 0 nodes in empty file, got %d", len(batHosts.Nodes))
	}
}

func TestParseBatHosts_NonExistentFile(t *testing.T) {
	_, err := ParseBatHostsFile("/nonexistent/bat-hosts")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

func TestGetNodeByMAC(t *testing.T) {
	testFilePath := testBatHostsFilePath

	batHosts, err := ParseBatHostsFile(testFilePath)
	if err != nil {
		t.Fatalf("Failed to parse bat-hosts file: %v", err)
	}

	tests := []struct {
		nodeMAC       string
		expectFound   bool
		expectedHosts int
	}{
		{"0a:d7:37:78:2d:3e", true, 5},
		{"2c:cf:67:b8:88:ba", true, 4},
		{"2c:cf:67:bb:10:03", true, 5},
		{"00:c0:ca:b6:5c:56", true, 4},
		{"e4:5f:01:df:f8:fd", true, 5},
		{"00:00:00:00:00:00", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.nodeMAC, func(t *testing.T) {
			node := batHosts.GetNodeByMAC(tt.nodeMAC)
			if tt.expectFound {
				if node == nil {
					t.Errorf("Expected to find node %s, but got nil", tt.nodeMAC)
				} else if len(node.Hosts) != tt.expectedHosts {
					t.Errorf("Node %s expected %d hosts, got %d", tt.nodeMAC, tt.expectedHosts, len(node.Hosts))
				}
			} else {
				if node != nil {
					t.Errorf("Expected not to find node %s, but got one", tt.nodeMAC)
				}
			}
		})
	}
}
