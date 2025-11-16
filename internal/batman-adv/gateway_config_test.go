package batmanadv

import (
	"encoding/json"
	"reflect"
	"testing"
)

// mockGatewaysJSON returns sample batctl gwj output
func mockGatewaysJSON() string {
	return `[
  {
    "hard_ifindex": 3,
    "hard_ifname": "wlan0",
    "orig_address": "aa:bb:cc:dd:ee:01",
    "best": true,
    "throughput": 10000,
    "bandwidth_up": 2000,
    "bandwidth_down": 10000,
    "router": "aa:bb:cc:dd:ee:01"
  },
  {
    "hard_ifindex": 4,
    "hard_ifname": "wlan1",
    "orig_address": "aa:bb:cc:dd:ee:02",
    "best": false,
    "throughput": 5000,
    "bandwidth_up": 1000,
    "bandwidth_down": 5000,
    "router": "aa:bb:cc:dd:ee:02"
  },
  {
    "hard_ifindex": 3,
    "hard_ifname": "wlan0",
    "orig_address": "aa:bb:cc:dd:ee:03",
    "best": false,
    "throughput": 7500,
    "bandwidth_up": 1500,
    "bandwidth_down": 7500,
    "router": "aa:bb:cc:dd:ee:03"
  }
]`
}

// createMockGateways creates a Gateways slice from mock JSON
func createMockGateways() *Gateways {
	var gateways Gateways
	json.Unmarshal([]byte(mockGatewaysJSON()), &gateways)
	return &gateways
}

func TestGetMeshGateways_Unmarshal(t *testing.T) {
	mockData := mockGatewaysJSON()

	var gateways []Gateway
	if err := json.Unmarshal([]byte(mockData), &gateways); err != nil {
		t.Fatalf("Failed to unmarshal mock data: %v", err)
	}

	if len(gateways) != 3 {
		t.Errorf("Expected 3 gateways, got %d", len(gateways))
	}

	// Verify first gateway
	if gateways[0].OrigAddress != "aa:bb:cc:dd:ee:01" {
		t.Errorf("Expected orig_address 'aa:bb:cc:dd:ee:01', got '%s'", gateways[0].OrigAddress)
	}
	if !gateways[0].Best {
		t.Error("Expected first gateway to be marked as best")
	}
	if gateways[0].Throughput != 10000 {
		t.Errorf("Expected throughput 10000, got %d", gateways[0].Throughput)
	}
}

func TestGetBest(t *testing.T) {
	tests := []struct {
		name     string
		gateways *Gateways
		wantAddr string
		wantNil  bool
	}{
		{
			name:     "with best gateway",
			gateways: createMockGateways(),
			wantAddr: "aa:bb:cc:dd:ee:01",
			wantNil:  false,
		},
		{
			name:     "nil gateways",
			gateways: nil,
			wantNil:  true,
		},
		{
			name:     "empty gateways",
			gateways: &Gateways{},
			wantNil:  true,
		},
		{
			name: "no best gateway",
			gateways: &Gateways{
				{OrigAddress: "aa:bb:cc:dd:ee:01", Best: false},
				{OrigAddress: "aa:bb:cc:dd:ee:02", Best: false},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gateways.GetBest()
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetBest() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("GetBest() = nil, want non-nil")
				}
				if got.OrigAddress != tt.wantAddr {
					t.Errorf("GetBest().OrigAddress = %v, want %v", got.OrigAddress, tt.wantAddr)
				}
			}
		})
	}
}

func TestFindByOrigAddress(t *testing.T) {
	gateways := createMockGateways()

	tests := []struct {
		name        string
		gateways    *Gateways
		origAddress string
		wantNil     bool
	}{
		{
			name:        "found",
			gateways:    gateways,
			origAddress: "aa:bb:cc:dd:ee:02",
			wantNil:     false,
		},
		{
			name:        "not found",
			gateways:    gateways,
			origAddress: "ff:ff:ff:ff:ff:ff",
			wantNil:     true,
		},
		{
			name:        "nil gateways",
			gateways:    nil,
			origAddress: "aa:bb:cc:dd:ee:01",
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gateways.FindByOrigAddress(tt.origAddress)
			if tt.wantNil {
				if got != nil {
					t.Errorf("FindByOrigAddress() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("FindByOrigAddress() = nil, want non-nil")
				}
				if got.OrigAddress != tt.origAddress {
					t.Errorf("FindByOrigAddress().OrigAddress = %v, want %v", got.OrigAddress, tt.origAddress)
				}
			}
		})
	}
}

func TestFindByInterface(t *testing.T) {
	gateways := createMockGateways()

	tests := []struct {
		name     string
		gateways *Gateways
		ifname   string
		wantNil  bool
	}{
		{
			name:     "found",
			gateways: gateways,
			ifname:   "wlan1",
			wantNil:  false,
		},
		{
			name:     "not found",
			gateways: gateways,
			ifname:   "eth0",
			wantNil:  true,
		},
		{
			name:     "nil gateways",
			gateways: nil,
			ifname:   "wlan0",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gateways.FindByInterface(tt.ifname)
			if tt.wantNil {
				if got != nil {
					t.Errorf("FindByInterface() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("FindByInterface() = nil, want non-nil")
				}
				if got.HardIfname != tt.ifname {
					t.Errorf("FindByInterface().HardIfname = %v, want %v", got.HardIfname, tt.ifname)
				}
			}
		})
	}
}

func TestFilterByInterface(t *testing.T) {
	gateways := createMockGateways()

	tests := []struct {
		name      string
		gateways  *Gateways
		ifname    string
		wantCount int
	}{
		{
			name:      "multiple matches",
			gateways:  gateways,
			ifname:    "wlan0",
			wantCount: 2,
		},
		{
			name:      "single match",
			gateways:  gateways,
			ifname:    "wlan1",
			wantCount: 1,
		},
		{
			name:      "no matches",
			gateways:  gateways,
			ifname:    "eth0",
			wantCount: 0,
		},
		{
			name:      "nil gateways",
			gateways:  nil,
			ifname:    "wlan0",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gateways.FilterByInterface(tt.ifname)
			if len(got) != tt.wantCount {
				t.Errorf("FilterByInterface() returned %d gateways, want %d", len(got), tt.wantCount)
			}
			for _, gw := range got {
				if gw.HardIfname != tt.ifname {
					t.Errorf("FilterByInterface() returned gateway with ifname %s, want %s", gw.HardIfname, tt.ifname)
				}
			}
		})
	}
}

func TestCount(t *testing.T) {
	tests := []struct {
		name     string
		gateways *Gateways
		want     int
	}{
		{
			name:     "with gateways",
			gateways: createMockGateways(),
			want:     3,
		},
		{
			name:     "empty",
			gateways: &Gateways{},
			want:     0,
		},
		{
			name:     "nil",
			gateways: nil,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gateways.Count(); got != tt.want {
				t.Errorf("Count() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		gateways *Gateways
		want     bool
	}{
		{
			name:     "with gateways",
			gateways: createMockGateways(),
			want:     false,
		},
		{
			name:     "empty",
			gateways: &Gateways{},
			want:     true,
		},
		{
			name:     "nil",
			gateways: nil,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gateways.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasBest(t *testing.T) {
	tests := []struct {
		name     string
		gateways *Gateways
		want     bool
	}{
		{
			name:     "has best",
			gateways: createMockGateways(),
			want:     true,
		},
		{
			name: "no best",
			gateways: &Gateways{
				{OrigAddress: "aa:bb:cc:dd:ee:01", Best: false},
			},
			want: false,
		},
		{
			name:     "nil",
			gateways: nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gateways.HasBest(); got != tt.want {
				t.Errorf("HasBest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetHighestThroughput(t *testing.T) {
	gateways := createMockGateways()

	tests := []struct {
		name           string
		gateways       *Gateways
		wantThroughput int
		wantNil        bool
	}{
		{
			name:           "highest throughput",
			gateways:       gateways,
			wantThroughput: 10000,
			wantNil:        false,
		},
		{
			name:     "nil gateways",
			gateways: nil,
			wantNil:  true,
		},
		{
			name:     "empty gateways",
			gateways: &Gateways{},
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gateways.GetHighestThroughput()
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

func TestSortByThroughput(t *testing.T) {
	gateways := createMockGateways()
	original := *gateways

	gateways.SortByThroughput()

	if len(*gateways) != len(original) {
		t.Errorf("SortByThroughput() changed gateway count")
	}

	// Verify descending order
	for i := 1; i < len(*gateways); i++ {
		if (*gateways)[i-1].Throughput < (*gateways)[i].Throughput {
			t.Errorf("SortByThroughput() not in descending order at index %d", i)
		}
	}

	// Verify highest throughput is first
	if (*gateways)[0].Throughput != 10000 {
		t.Errorf("SortByThroughput() first gateway throughput = %d, want 10000", (*gateways)[0].Throughput)
	}

	// Test nil gateways
	var nilGateways *Gateways
	nilGateways.SortByThroughput() // Should not panic
}

func TestSortByOrigAddress(t *testing.T) {
	gateways := createMockGateways()
	original := *gateways

	gateways.SortByOrigAddress()

	if len(*gateways) != len(original) {
		t.Errorf("SortByOrigAddress() changed gateway count")
	}

	// Verify ascending order
	for i := 1; i < len(*gateways); i++ {
		if (*gateways)[i-1].OrigAddress > (*gateways)[i].OrigAddress {
			t.Errorf("SortByOrigAddress() not in ascending order at index %d", i)
		}
	}

	// Test nil gateways
	var nilGateways *Gateways
	nilGateways.SortByOrigAddress() // Should not panic
}

func TestGetOrigAddresses(t *testing.T) {
	gateways := createMockGateways()

	addresses := gateways.GetOrigAddresses()

	if len(addresses) != 3 {
		t.Errorf("GetOrigAddresses() returned %d addresses, want 3", len(addresses))
	}

	expected := []string{"aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:03"}
	if !reflect.DeepEqual(addresses, expected) {
		t.Errorf("GetOrigAddresses() = %v, want %v", addresses, expected)
	}

	// Test nil gateways
	var nilGateways *Gateways
	if addresses := nilGateways.GetOrigAddresses(); len(addresses) != 0 {
		t.Errorf("GetOrigAddresses() on nil = %v, want []", addresses)
	}
}

func TestGetInterfaces(t *testing.T) {
	gateways := createMockGateways()

	interfaces := gateways.GetInterfaces()

	if len(interfaces) != 2 {
		t.Errorf("GetInterfaces() returned %d interfaces, want 2", len(interfaces))
	}

	expected := []string{"wlan0", "wlan1"}
	if !reflect.DeepEqual(interfaces, expected) {
		t.Errorf("GetInterfaces() = %v, want %v", interfaces, expected)
	}

	// Verify sorted
	for i := 1; i < len(interfaces); i++ {
		if interfaces[i-1] > interfaces[i] {
			t.Error("GetInterfaces() returned unsorted list")
		}
	}

	// Test nil gateways
	var nilGateways *Gateways
	if interfaces := nilGateways.GetInterfaces(); len(interfaces) != 0 {
		t.Errorf("GetInterfaces() on nil = %v, want []", interfaces)
	}
}

func TestTotalThroughput(t *testing.T) {
	tests := []struct {
		name     string
		gateways *Gateways
		want     int
	}{
		{
			name:     "sum of all",
			gateways: createMockGateways(),
			want:     22500, // 10000 + 5000 + 7500
		},
		{
			name:     "nil",
			gateways: nil,
			want:     0,
		},
		{
			name:     "empty",
			gateways: &Gateways{},
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gateways.TotalThroughput(); got != tt.want {
				t.Errorf("TotalThroughput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAverageThroughput(t *testing.T) {
	tests := []struct {
		name     string
		gateways *Gateways
		want     float64
	}{
		{
			name:     "average of all",
			gateways: createMockGateways(),
			want:     7500.0, // 22500 / 3
		},
		{
			name:     "nil",
			gateways: nil,
			want:     0.0,
		},
		{
			name:     "empty",
			gateways: &Gateways{},
			want:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gateways.AverageThroughput(); got != tt.want {
				t.Errorf("AverageThroughput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGateways_String(t *testing.T) {
	tests := []struct {
		name     string
		gateways *Gateways
		wantJSON bool
	}{
		{
			name:     "with gateways",
			gateways: createMockGateways(),
			wantJSON: true,
		},
		{
			name:     "empty",
			gateways: &Gateways{},
			wantJSON: true,
		},
		{
			name:     "nil",
			gateways: nil,
			wantJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gateways.String()
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

func TestGateway_AllFields(t *testing.T) {
	// Test that all Gateway fields can be set and retrieved
	gw := Gateway{
		HardIfindex:   42,
		HardIfname:    "test-iface",
		OrigAddress:   "aa:bb:cc:dd:ee:ff",
		Best:          true,
		Throughput:    12345,
		BandwidthUp:   1000,
		BandwidthDown: 5000,
		Router:        "aa:bb:cc:dd:ee:ff",
	}

	if gw.HardIfindex != 42 {
		t.Errorf("HardIfindex = %d, want 42", gw.HardIfindex)
	}
	if gw.HardIfname != "test-iface" {
		t.Errorf("HardIfname = %s, want test-iface", gw.HardIfname)
	}
	if gw.OrigAddress != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("OrigAddress = %s, want aa:bb:cc:dd:ee:ff", gw.OrigAddress)
	}
	if !gw.Best {
		t.Error("Best should be true")
	}
	if gw.Throughput != 12345 {
		t.Errorf("Throughput = %d, want 12345", gw.Throughput)
	}
	if gw.BandwidthUp != 1000 {
		t.Errorf("BandwidthUp = %d, want 1000", gw.BandwidthUp)
	}
	if gw.BandwidthDown != 5000 {
		t.Errorf("BandwidthDown = %d, want 5000", gw.BandwidthDown)
	}
	if gw.Router != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("Router = %s, want aa:bb:cc:dd:ee:ff", gw.Router)
	}
}
