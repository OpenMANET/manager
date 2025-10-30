package network

import (
	"net"
	"testing"
)

func TestGetInterfaceByName(t *testing.T) {
	// Get a real interface for testing
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) == 0 {
		t.Skip("No network interfaces available for testing")
	}

	t.Run("existing interface", func(t *testing.T) {
		iface := interfaces[0]
		result := GetInterfaceByName(iface.Name)

		if result.Name != iface.Name {
			t.Errorf("Expected name %s, got %s", iface.Name, result.Name)
		}
		if result.MTU != iface.MTU {
			t.Errorf("Expected MTU %d, got %d", iface.MTU, result.MTU)
		}
		if result.MAC != iface.HardwareAddr.String() {
			t.Errorf("Expected MAC %s, got %s", iface.HardwareAddr.String(), result.MAC)
		}
	})

	t.Run("non-existing interface", func(t *testing.T) {
		result := GetInterfaceByName("nonexistent123")
		if result.Name != "" {
			t.Errorf("Expected empty InterfaceInfo, got %+v", result)
		}
	})
}

func TestCalculateBroadcastAddress(t *testing.T) {
	tests := []struct {
		name      string
		ipNet     *net.IPNet
		wantIP    string
		wantIsNil bool
	}{
		{
			name: "192.168.1.10/24",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("192.168.1.10"),
				Mask: net.CIDRMask(24, 32),
			},
			wantIP:    "192.168.1.255",
			wantIsNil: false,
		},
		{
			name: "10.0.0.1/8",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("10.0.0.1"),
				Mask: net.CIDRMask(8, 32),
			},
			wantIP:    "10.255.255.255",
			wantIsNil: false,
		},
		{
			name: "172.16.0.1/16",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("172.16.0.1"),
				Mask: net.CIDRMask(16, 32),
			},
			wantIP:    "172.16.255.255",
			wantIsNil: false,
		},
		{
			name: "IPv6 address",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("2001:db8::1"),
				Mask: net.CIDRMask(64, 128),
			},
			wantIsNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateBroadcastAddress(tt.ipNet)
			
			if tt.wantIsNil {
				if result != nil {
					t.Errorf("Expected nil for IPv6, got %v", result)
				}
			} else {
				if result == nil {
					t.Fatal("Expected broadcast address, got nil")
				}
				if result.String() != tt.wantIP {
					t.Errorf("Expected broadcast %s, got %s", tt.wantIP, result.String())
				}
			}
		})
	}
}

func TestGetInterfaceIPAddresses(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) == 0 {
		t.Skip("No network interfaces available for testing")
	}

	// Find an interface with at least one IP address
	var testIface net.Interface
	found := false
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err == nil && len(addrs) > 0 {
			testIface = iface
			found = true
			break
		}
	}

	if !found {
		t.Skip("No interface with IP addresses found")
	}

	t.Run("interface with IP addresses", func(t *testing.T) {
		ipAddresses := getInterfaceIPAddresses(testIface)

		if len(ipAddresses) == 0 {
			t.Error("Expected at least one IP address")
		}

		for _, ipAddr := range ipAddresses {
			if ipAddr.IP == nil {
				t.Error("IP should not be nil")
			}
		}
	})
}
func TestCalculateBroadcastAddressEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		ipNet     *net.IPNet
		wantIP    string
		wantIsNil bool
	}{
		{
			name: "192.168.0.1/32 - single host",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("192.168.0.1"),
				Mask: net.CIDRMask(32, 32),
			},
			wantIP:    "192.168.0.1",
			wantIsNil: false,
		},
		{
			name: "0.0.0.0/0 - entire IPv4 space",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("0.0.0.0"),
				Mask: net.CIDRMask(0, 32),
			},
			wantIP:    "255.255.255.255",
			wantIsNil: false,
		},
		{
			name: "192.168.1.128/25 - half subnet",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("192.168.1.128"),
				Mask: net.CIDRMask(25, 32),
			},
			wantIP:    "192.168.1.255",
			wantIsNil: false,
		},
		{
			name: "10.20.30.40/30 - small subnet",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("10.20.30.40"),
				Mask: net.CIDRMask(30, 32),
			},
			wantIP:    "10.20.30.43",
			wantIsNil: false,
		},
		{
			name: "nil IP in IPNet",
			ipNet: &net.IPNet{
				IP:   nil,
				Mask: net.CIDRMask(24, 32),
			},
			wantIsNil: true,
		},
		{
			name: "IPv6 with To4 conversion failure",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("fe80::1"),
				Mask: net.CIDRMask(64, 128),
			},
			wantIsNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateBroadcastAddress(tt.ipNet)

			if tt.wantIsNil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
			} else {
				if result == nil {
					t.Fatal("Expected broadcast address, got nil")
				}
				if result.String() != tt.wantIP {
					t.Errorf("Expected broadcast %s, got %s", tt.wantIP, result.String())
				}
			}
		})
	}
}
func TestGetInterfaceByNameFlags(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) == 0 {
		t.Skip("No network interfaces available for testing")
	}

	t.Run("flags are correctly copied", func(t *testing.T) {
		iface := interfaces[0]
		result := GetInterfaceByName(iface.Name)

		if result.Flags != iface.Flags {
			t.Errorf("Expected Flags %v, got %v", iface.Flags, result.Flags)
		}
	})
}

func TestGetInterfaceByNameIPAddresses(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) == 0 {
		t.Skip("No network interfaces available for testing")
	}

	// Find an interface with at least one IP address
	var testIface net.Interface
	found := false
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err == nil && len(addrs) > 0 {
			testIface = iface
			found = true
			break
		}
	}

	if !found {
		t.Skip("No interface with IP addresses found")
	}

	t.Run("IP addresses are populated", func(t *testing.T) {
		result := GetInterfaceByName(testIface.Name)

		if len(result.IP) == 0 {
			t.Error("Expected at least one IP address to be populated")
		}

		for i, ipAddr := range result.IP {
			if ipAddr.IP == nil {
				t.Errorf("IP address at index %d should not be nil", i)
			}
		}
	})
}

func TestGetInterfaceByNameEmptyString(t *testing.T) {
	t.Run("empty interface name", func(t *testing.T) {
		result := GetInterfaceByName("")
		if result.Name != "" {
			t.Errorf("Expected empty InterfaceInfo for empty name, got %+v", result)
		}
		if result.MTU != 0 {
			t.Errorf("Expected MTU 0, got %d", result.MTU)
		}
		if result.MAC != "" {
			t.Errorf("Expected empty MAC, got %s", result.MAC)
		}
		if len(result.IP) != 0 {
			t.Errorf("Expected no IP addresses, got %d", len(result.IP))
		}
	})
}

func TestGetInterfaceByNameSpecialCharacters(t *testing.T) {
	t.Run("interface name with special characters", func(t *testing.T) {
		result := GetInterfaceByName("eth0:1")
		if result.Name != "" {
			// This will be empty unless such an interface actually exists
			t.Logf("Found interface with special character: %+v", result)
		}
	})
}

func TestGetInterfaceByNameCaseSensitivity(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) == 0 {
		t.Skip("No network interfaces available for testing")
	}

	t.Run("case sensitivity check", func(t *testing.T) {
		iface := interfaces[0]
		if iface.Name == "" {
			t.Skip("Interface has empty name")
		}

		// Try uppercase version
		upperName := ""
		hasLower := false
		for _, c := range iface.Name {
			if c >= 'a' && c <= 'z' {
				hasLower = true
				upperName += string(c - 32)
			} else {
				upperName += string(c)
			}
		}

		if !hasLower {
			t.Skip("Interface name has no lowercase letters")
		}

		result := GetInterfaceByName(upperName)
		if result.Name == iface.Name {
			t.Error("Expected case-sensitive match to fail, but it succeeded")
		}
	})
}


