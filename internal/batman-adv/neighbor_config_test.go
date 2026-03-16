package batmanadv

import (
	"encoding/json"
	"reflect"
	"testing"
)

// mockNeighborsJSON returns sample batctl nj output
func mockNeighborsJSON() string {
	return `[
  {
    "hard_ifindex": 8,
    "hard_ifname": "phy1-mesh0",
    "last_seen_msecs": 150,
    "neigh_address": "9c:ef:d5:f9:80:4d",
    "throughput": 2400
  },
  {
    "hard_ifindex": 8,
    "hard_ifname": "phy1-mesh0",
    "last_seen_msecs": 90,
    "neigh_address": "02:c0:ca:b6:5c:58",
    "throughput": 16100
  },
  {
    "hard_ifindex": 11,
    "hard_ifname": "wlan0",
    "last_seen_msecs": 70,
    "neigh_address": "bc:2a:33:96:b1:84",
    "throughput": 7100
  }
]`
}

// createMockNeighbors creates a Neighbors slice from mock JSON
func createMockNeighbors() *Neighbors {
	var neighbors Neighbors
	_ = json.Unmarshal([]byte(mockNeighborsJSON()), &neighbors)

	return &neighbors
}

func TestGetMeshNeighbors_Unmarshal(t *testing.T) {
	mockData := mockNeighborsJSON()

	var neighbors []Neighbor
	if err := json.Unmarshal([]byte(mockData), &neighbors); err != nil {
		t.Fatalf("Failed to unmarshal mock data: %v", err)
	}

	if len(neighbors) != 3 {
		t.Errorf("Expected 3 neighbors, got %d", len(neighbors))
	}

	// Verify first neighbor
	if neighbors[0].HardIfindex != 8 {
		t.Errorf("Expected hard_ifindex 8, got %d", neighbors[0].HardIfindex)
	}
	if neighbors[0].HardIfname != "phy1-mesh0" {
		t.Errorf("Expected hard_ifname 'phy1-mesh0', got '%s'", neighbors[0].HardIfname)
	}
	if neighbors[0].LastSeenMsecs != 150 {
		t.Errorf("Expected last_seen_msecs 150, got %d", neighbors[0].LastSeenMsecs)
	}
	if neighbors[0].NeighAddress != "9c:ef:d5:f9:80:4d" {
		t.Errorf("Expected neigh_address '9c:ef:d5:f9:80:4d', got '%s'", neighbors[0].NeighAddress)
	}
	if neighbors[0].Throughput != 2400 {
		t.Errorf("Expected throughput 2400, got %d", neighbors[0].Throughput)
	}

	// Verify third neighbor is on different interface
	if neighbors[2].HardIfname != "wlan0" {
		t.Errorf("Expected hard_ifname 'wlan0', got '%s'", neighbors[2].HardIfname)
	}
}

func TestFindByNeighAddress(t *testing.T) {
	neighbors := createMockNeighbors()

	tests := []struct {
		name      string
		neighbors *Neighbors
		mac       string
		wantNil   bool
	}{
		{
			name:      "found",
			neighbors: neighbors,
			mac:       "9c:ef:d5:f9:80:4d",
			wantNil:   false,
		},
		{
			name:      "found case insensitive",
			neighbors: neighbors,
			mac:       "9C:EF:D5:F9:80:4D",
			wantNil:   false,
		},
		{
			name:      "not found",
			neighbors: neighbors,
			mac:       "ff:ff:ff:ff:ff:ff",
			wantNil:   true,
		},
		{
			name:      "nil neighbors",
			neighbors: nil,
			mac:       "9c:ef:d5:f9:80:4d",
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.neighbors.FindByNeighAddress(tt.mac)
			if tt.wantNil {
				if got != nil {
					t.Errorf("FindByNeighAddress() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("FindByNeighAddress() = nil, want non-nil")
				}
				if got.NeighAddress != "9c:ef:d5:f9:80:4d" {
					t.Errorf("FindByNeighAddress().NeighAddress = %v, want 9c:ef:d5:f9:80:4d", got.NeighAddress)
				}
			}
		})
	}
}

func TestNeighbors_FilterByInterface(t *testing.T) {
	neighbors := createMockNeighbors()

	tests := []struct {
		name      string
		neighbors *Neighbors
		ifname    string
		wantCount int
	}{
		{
			name:      "multiple matches",
			neighbors: neighbors,
			ifname:    "phy1-mesh0",
			wantCount: 2,
		},
		{
			name:      "single match",
			neighbors: neighbors,
			ifname:    "wlan0",
			wantCount: 1,
		},
		{
			name:      "no matches",
			neighbors: neighbors,
			ifname:    "eth0",
			wantCount: 0,
		},
		{
			name:      "nil neighbors",
			neighbors: nil,
			ifname:    "phy1-mesh0",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.neighbors.FilterByInterface(tt.ifname)
			if len(got) != tt.wantCount {
				t.Errorf("FilterByInterface() returned %d neighbors, want %d", len(got), tt.wantCount)
			}
			for _, n := range got {
				if n.HardIfname != tt.ifname {
					t.Errorf("FilterByInterface() returned neighbor with ifname %s, want %s", n.HardIfname, tt.ifname)
				}
			}
		})
	}
}

func TestNeighbors_Count(t *testing.T) {
	tests := []struct {
		name      string
		neighbors *Neighbors
		want      int
	}{
		{
			name:      "with neighbors",
			neighbors: createMockNeighbors(),
			want:      3,
		},
		{
			name:      "empty",
			neighbors: &Neighbors{},
			want:      0,
		},
		{
			name:      "nil",
			neighbors: nil,
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.neighbors.Count(); got != tt.want {
				t.Errorf("Count() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeighbors_IsEmpty(t *testing.T) {
	tests := []struct {
		name      string
		neighbors *Neighbors
		want      bool
	}{
		{
			name:      "with neighbors",
			neighbors: createMockNeighbors(),
			want:      false,
		},
		{
			name:      "empty",
			neighbors: &Neighbors{},
			want:      true,
		},
		{
			name:      "nil",
			neighbors: nil,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.neighbors.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeighbors_GetNeighAddresses(t *testing.T) {
	neighbors := createMockNeighbors()

	addresses := neighbors.GetNeighAddresses()

	if len(addresses) != 3 {
		t.Errorf("GetNeighAddresses() returned %d addresses, want 3", len(addresses))
	}

	expected := []string{"9c:ef:d5:f9:80:4d", "02:c0:ca:b6:5c:58", "bc:2a:33:96:b1:84"}
	if !reflect.DeepEqual(addresses, expected) {
		t.Errorf("GetNeighAddresses() = %v, want %v", addresses, expected)
	}

	// Test nil neighbors
	var nilNeighbors *Neighbors
	if addresses := nilNeighbors.GetNeighAddresses(); len(addresses) != 0 {
		t.Errorf("GetNeighAddresses() on nil = %v, want []", addresses)
	}
}

func TestNeighbors_GetInterfaces(t *testing.T) {
	neighbors := createMockNeighbors()

	interfaces := neighbors.GetInterfaces()

	if len(interfaces) != 2 {
		t.Errorf("GetInterfaces() returned %d interfaces, want 2", len(interfaces))
	}

	expected := []string{"phy1-mesh0", "wlan0"}
	if !reflect.DeepEqual(interfaces, expected) {
		t.Errorf("GetInterfaces() = %v, want %v", interfaces, expected)
	}

	// Verify sorted
	for i := 1; i < len(interfaces); i++ {
		if interfaces[i-1] > interfaces[i] {
			t.Error("GetInterfaces() returned unsorted list")
		}
	}

	// Test nil neighbors
	var nilNeighbors *Neighbors
	if interfaces := nilNeighbors.GetInterfaces(); len(interfaces) != 0 {
		t.Errorf("GetInterfaces() on nil = %v, want []", interfaces)
	}
}

func TestNeighbors_GetHighestThroughput(t *testing.T) {
	neighbors := createMockNeighbors()

	tests := []struct {
		name           string
		neighbors      *Neighbors
		wantThroughput int
		wantNil        bool
	}{
		{
			name:           "highest throughput",
			neighbors:      neighbors,
			wantThroughput: 16100,
			wantNil:        false,
		},
		{
			name:      "nil neighbors",
			neighbors: nil,
			wantNil:   true,
		},
		{
			name:      "empty neighbors",
			neighbors: &Neighbors{},
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.neighbors.GetHighestThroughput()
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetHighestThroughput() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("GetHighestThroughput() = nil, want non-nil")
				}
				if got.Throughput != tt.wantThroughput {
					t.Errorf("GetHighestThroughput().Throughput = %v, want %v", got.Throughput, tt.wantThroughput)
				}
			}
		})
	}
}

func TestNeighbors_SortByThroughput(t *testing.T) {
	neighbors := createMockNeighbors()
	original := *neighbors

	neighbors.SortByThroughput()

	if len(*neighbors) != len(original) {
		t.Errorf("SortByThroughput() changed neighbor count")
	}

	// Verify descending order
	for i := 1; i < len(*neighbors); i++ {
		if (*neighbors)[i-1].Throughput < (*neighbors)[i].Throughput {
			t.Errorf("SortByThroughput() not in descending order at index %d", i)
		}
	}

	// Verify highest throughput is first
	if (*neighbors)[0].Throughput != 16100 {
		t.Errorf("SortByThroughput() first neighbor throughput = %d, want 16100", (*neighbors)[0].Throughput)
	}

	// Test nil neighbors
	var nilNeighbors *Neighbors
	nilNeighbors.SortByThroughput() // Should not panic
}

func TestNeighbors_SortByLastSeen(t *testing.T) {
	neighbors := createMockNeighbors()
	original := *neighbors

	neighbors.SortByLastSeen()

	if len(*neighbors) != len(original) {
		t.Errorf("SortByLastSeen() changed neighbor count")
	}

	// Verify ascending order (most recently seen first)
	for i := 1; i < len(*neighbors); i++ {
		if (*neighbors)[i-1].LastSeenMsecs > (*neighbors)[i].LastSeenMsecs {
			t.Errorf("SortByLastSeen() not in ascending order at index %d", i)
		}
	}

	// Verify lowest last_seen_msecs is first (most recently seen)
	if (*neighbors)[0].LastSeenMsecs != 70 {
		t.Errorf("SortByLastSeen() first neighbor last_seen_msecs = %d, want 70", (*neighbors)[0].LastSeenMsecs)
	}

	// Test nil neighbors
	var nilNeighbors *Neighbors
	nilNeighbors.SortByLastSeen() // Should not panic
}

func TestNeighbors_String(t *testing.T) {
	tests := []struct {
		name      string
		neighbors *Neighbors
	}{
		{
			name:      "with neighbors",
			neighbors: createMockNeighbors(),
		},
		{
			name:      "empty",
			neighbors: &Neighbors{},
		},
		{
			name:      "nil",
			neighbors: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.neighbors.String()
			if got == "" {
				t.Error("String() returned empty string")
			}

			// Verify valid JSON
			var decoded interface{}
			if err := json.Unmarshal([]byte(got), &decoded); err != nil {
				t.Errorf("String() returned invalid JSON: %v", err)
			}
		})
	}
}

func TestNeighbor_AllFields(t *testing.T) {
	n := Neighbor{
		HardIfindex:   42,
		HardIfname:    "test-iface",
		LastSeenMsecs: 500,
		NeighAddress:  "aa:bb:cc:dd:ee:ff",
		Throughput:    12345,
	}

	if n.HardIfindex != 42 {
		t.Errorf("HardIfindex = %d, want 42", n.HardIfindex)
	}
	if n.HardIfname != "test-iface" {
		t.Errorf("HardIfname = %s, want test-iface", n.HardIfname)
	}
	if n.LastSeenMsecs != 500 {
		t.Errorf("LastSeenMsecs = %d, want 500", n.LastSeenMsecs)
	}
	if n.NeighAddress != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("NeighAddress = %s, want aa:bb:cc:dd:ee:ff", n.NeighAddress)
	}
	if n.Throughput != 12345 {
		t.Errorf("Throughput = %d, want 12345", n.Throughput)
	}
}
