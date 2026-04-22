package batmanadv

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

const testVisJsondocPath = "../../testfixtures/batman-adv/vis-jsondoc.json"

func TestVisDoc_Unmarshal(t *testing.T) {
	raw, err := os.ReadFile(testVisJsondocPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var doc VisDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if doc.SourceVersion != "2013.3.0-14-gcd34783" {
		t.Errorf("SourceVersion = %q", doc.SourceVersion)
	}

	if doc.Algorithm != 4 {
		t.Errorf("Algorithm = %d", doc.Algorithm)
	}

	if len(doc.Vis) != 2 {
		t.Fatalf("len(Vis) = %d, want 2", len(doc.Vis))
	}

	first := doc.Vis[0]
	if first.Primary != "0a:d7:37:78:2d:3e" {
		t.Errorf("first.Primary = %q", first.Primary)
	}

	if len(first.Secondary) != 1 || first.Secondary[0] != "2c:cf:67:6a:97:d9" {
		t.Errorf("first.Secondary = %v", first.Secondary)
	}

	if len(first.Neighbors) != 2 {
		t.Errorf("first.Neighbors len = %d", len(first.Neighbors))
	}

	if first.Neighbors[0].Metric != "1.008" {
		t.Errorf("first metric = %q", first.Neighbors[0].Metric)
	}

	if len(first.Clients) != 2 {
		t.Errorf("first.Clients len = %d", len(first.Clients))
	}

	// Second entry has no "secondary" key — confirm the optional branch.
	if doc.Vis[1].Secondary != nil {
		t.Errorf("second.Secondary = %v, want nil", doc.Vis[1].Secondary)
	}
}

func TestVisDoc_Unmarshal_SecondaryOptional(t *testing.T) {
	raw := []byte(`{
	"source_version": "x",
	"algorithm": 4,
	"vis": [{"primary": "aa:bb:cc:dd:ee:ff", "neighbors": [], "clients": []}]
}`)

	var doc VisDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(doc.Vis) != 1 {
		t.Fatalf("len(Vis) = %d", len(doc.Vis))
	}

	if doc.Vis[0].Secondary != nil {
		t.Errorf("Secondary = %v, want nil when key is absent", doc.Vis[0].Secondary)
	}
}

func TestParseMetric(t *testing.T) {
	tests := []struct {
		in   string
		want float32
	}{
		{"1.008", 1.008},
		{"  1.000  ", 1.000},
		{"0.500", 0.500},
		{"", 0},
		{"not-a-number", 0},
	}

	for _, tt := range tests {
		got := ParseMetric(tt.in)
		if math.Abs(float64(got-tt.want)) > 1e-4 {
			t.Errorf("ParseMetric(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
