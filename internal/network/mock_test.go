package network

import (
	"testing"
)

func TestMockConfigReader_AnonymousSections(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
	}

	// Add three anonymous vxlan_peer sections
	for i := 0; i < 3; i++ {
		if err := reader.AddSection("network", "", "vxlan_peer"); err != nil {
			t.Fatalf("AddSection failed: %v", err)
		}
	}

	// Get the sections
	sections, err := reader.GetSections("network", "vxlan_peer")
	if err != nil {
		t.Fatalf("GetSections failed: %v", err)
	}

	// Should return 3 sections with proper @ notation
	if len(sections) != 3 {
		t.Errorf("Expected 3 sections, got %d: %v", len(sections), sections)
	}

	expectedSections := []string{"@vxlan_peer[0]", "@vxlan_peer[1]", "@vxlan_peer[2]"}
	for i, expected := range expectedSections {
		if i >= len(sections) {
			t.Errorf("Missing section %s", expected)

			continue
		}

		if sections[i] != expected {
			t.Errorf("Expected section[%d] to be %q, got %q", i, expected, sections[i])
		}
	}

	// Test SetType with @ notation
	err = reader.SetType("network", "@vxlan_peer[0]", "dst", 0, "10.0.0.1")
	if err != nil {
		t.Fatalf("SetType failed: %v", err)
	}

	// Verify the value was set on the internal key
	actualKey := reader.resolveSectionRef("network", "@vxlan_peer[0]")
	if actualKey == "" || actualKey == "@vxlan_peer[0]" {
		t.Errorf("resolveSectionRef didn't resolve the reference, got %q", actualKey)
	}

	// Verify data was set
	if val, ok := reader.data["network"][actualKey]["dst"]; !ok || len(val) == 0 || val[0] != "10.0.0.1" {
		t.Errorf("Expected dst to be set to '10.0.0.1', got %v", val)
	}
}
