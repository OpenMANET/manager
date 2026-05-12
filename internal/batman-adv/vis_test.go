package batmanadv_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVisDoc_Fixture(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(fixtureDir(), "vis-jsondoc.json"))
	require.NoError(t, err)

	doc, err := batmanadv.ParseVisDoc(b)
	require.NoError(t, err)

	assert.Equal(t, "2025.4", doc.SourceVersion)
	assert.Equal(t, 15, doc.Algorithm)
	assert.Equal(t, "BATMAN_V", doc.AlgorithmLabel())
	assert.Len(t, doc.Vis, 7)

	// Spot-check: the fixture includes a MAC written in mixed case on the
	// "bb:bb:bb:bb:bb:00" node's neighbor list; the parser must lower-case it
	// so downstream set operations don't have to ToLower on every lookup.
	var bbNode *batmanadv.VisNode

	for i := range doc.Vis {
		if doc.Vis[i].Primary == "bb:bb:bb:bb:bb:00" {
			bbNode = &doc.Vis[i]

			break
		}
	}

	require.NotNil(t, bbNode, "bb:bb:bb:bb:bb:00 node must be present")

	for _, n := range bbNode.Neighbors {
		assert.Equal(t, n.Router, filepath.Base(n.Router), "sanity")
		assert.NotContains(t, n.Neighbor, "A", "neighbor MAC must be lowercased")
	}

	// Clients list parsed + lowercased on the node that reports 3 clients.
	assert.ElementsMatch(t, []string{
		"f0:0d:00:00:00:01",
		"f0:0d:00:00:00:02",
		"f0:0d:00:00:00:03",
	}, bbNode.Clients)

	// Secondary list round-trips from the "aa:aa:aa:aa:aa:00" node.
	var aaNode *batmanadv.VisNode

	for i := range doc.Vis {
		if doc.Vis[i].Primary == "aa:aa:aa:aa:aa:00" {
			aaNode = &doc.Vis[i]

			break
		}
	}

	require.NotNil(t, aaNode)
	assert.Equal(t, []string{"aa:aa:aa:aa:aa:01"}, aaNode.Secondary)
}

func TestParseVisDoc_EmptyArrayReturnsUnavailable(t *testing.T) {
	_, err := batmanadv.ParseVisDoc([]byte(`{"source_version":"2025.4","algorithm":4,"vis":[]}`))
	assert.ErrorIs(t, err, batmanadv.ErrVisUnavailable)
}

func TestParseVisDoc_Malformed(t *testing.T) {
	_, err := batmanadv.ParseVisDoc([]byte(`not json`))
	require.Error(t, err)
	assert.False(t, errors.Is(err, batmanadv.ErrVisUnavailable))
}

func TestAlgorithmLabel(t *testing.T) {
	cases := []struct {
		algo int
		want string
	}{
		{4, "BATMAN_IV"},
		{15, "BATMAN_V"},
		{0, ""},
		{999, ""},
	}
	for _, tc := range cases {
		doc := &batmanadv.VisDoc{Algorithm: tc.algo}
		assert.Equal(t, tc.want, doc.AlgorithmLabel())
	}
}

func TestParseMetric(t *testing.T) {
	cases := map[string]float64{
		"":       0,
		"0":      0,
		"1.5":    1.5,
		"255":    255,
		"0.004":  0.004,
		"abc":    0,
		"1.5xyz": 0,
	}
	for in, want := range cases {
		assert.InDelta(t, want, batmanadv.ParseMetric(in), 1e-9, "input=%q", in)
	}
}
