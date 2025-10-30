package network

import (
	"reflect"
	"testing"
)

// mockConfigReader is a test double that returns predefined configuration values.
type mockConfigReader struct {
	data map[string]map[string]map[string][]string
}

func (m *mockConfigReader) Get(config, section, option string) ([]string, bool) {
	if configData, ok := m.data[config]; ok {
		if sectionData, ok := configData[section]; ok {
			if values, ok := sectionData[option]; ok {
				return values, true
			}
		}
	}
	return nil, false
}

func newMockReader() *mockConfigReader {
	return &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {
				"loopback": {
					"device":  {"lo"},
					"proto":   {"static"},
					"ipaddr":  {"127.0.0.1"},
					"netmask": {"255.0.0.0"},
				},
				"lan": {
					"proto":   {"static"},
					"ipaddr":  {"10.42.0.1"},
					"netmask": {"255.255.255.0"},
					"dns":     {"1.1.1.1"},
				},
				"wan": {
					"proto": {"dhcp"},
				},
				"ahwlan": {
					"proto":   {"static"},
					"netmask": {"255.255.0.0"},
					"ipaddr":  {"10.41.237.1"},
					"dns":     {"1.1.1.1"},
					"device":  {"br-ahwlan"},
					"gateway": {"10.41.1.1"},
				},
				"bat0": {
					"proto": {"batadv"},
				},
			},
		},
	}
}

func TestGetUCINetworkByNameWithReader_Loopback(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto:   "static",
		NetMask: "255.0.0.0",
		IPAddr:  "127.0.0.1",
		Device:  "lo",
	}

	got, err := GetUCINetworkByNameWithReader("loopback", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_LAN(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto:   "static",
		NetMask: "255.255.255.0",
		IPAddr:  "10.42.0.1",
		DNS:     "1.1.1.1",
	}

	got, err := GetUCINetworkByNameWithReader("lan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_WAN(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto: "dhcp",
	}

	got, err := GetUCINetworkByNameWithReader("wan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_AHWLAN(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto:   "static",
		NetMask: "255.255.0.0",
		IPAddr:  "10.41.237.1",
		Gateway: "10.41.1.1",
		DNS:     "1.1.1.1",
		Device:  "br-ahwlan",
	}

	got, err := GetUCINetworkByNameWithReader("ahwlan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_NonExistent(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{}

	got, err := GetUCINetworkByNameWithReader("nonexistent", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_Bat0(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto: "batadv",
	}

	got, err := GetUCINetworkByNameWithReader("bat0", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
