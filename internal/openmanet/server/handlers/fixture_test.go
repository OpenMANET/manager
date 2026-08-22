package handlers_test

// fixture_test.go — parse the captured LuCI after-state fixtures under
// testfixtures/setup-wizard/ and compare a staged uciTree section's
// full option set against a fixture section. This is the closest
// achievable form of "fixture equivalence": order-insensitive,
// section-name-tolerant (fixtures use anonymous sections where the
// wizard must use named ones — go-uci AddSection("") limitation),
// but exhaustive over options. Operator-set fields outside wizard
// scope are excluded per call via ignoreOptions.

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixtureSection struct {
	Type    string
	Name    string // "" for anonymous sections
	Options map[string][]string
}

// fixtureRoot walks up from this file to the module root, mirroring
// regdbFixturePath.
func fixtureRoot(t *testing.T) string {
	t.Helper()

	_, here, _, ok := runtime.Caller(0)
	require.True(t, ok)

	root := here
	for range 5 {
		root = filepath.Dir(root)
	}

	return filepath.Join(root, "testfixtures", "setup-wizard")
}

// loadFixture parses testfixtures/setup-wizard/after/<scenario>/<config>.
func loadFixture(t *testing.T, scenario, config string) []fixtureSection {
	t.Helper()

	path := filepath.Join(fixtureRoot(t), "after", scenario, config)
	f, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	var out []fixtureSection

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		switch {
		case strings.HasPrefix(line, "config "):
			rest := strings.TrimPrefix(line, "config ")
			fields := strings.SplitN(rest, " ", 2)

			sec := fixtureSection{Type: fields[0], Options: map[string][]string{}}
			if len(fields) == 2 {
				sec.Name = strings.Trim(fields[1], "'")
			}

			out = append(out, sec)
		case strings.HasPrefix(line, "option "), strings.HasPrefix(line, "list "):
			require.NotEmpty(t, out, "option before any config stanza in %s", path)

			rest := strings.TrimPrefix(strings.TrimPrefix(line, "option "), "list ")
			kv := strings.SplitN(rest, " ", 2)
			require.Len(t, kv, 2, "malformed line %q in %s", line, path)
			key := kv[0]
			val := strings.Trim(kv[1], "'")
			last := &out[len(out)-1]
			last.Options[key] = append(last.Options[key], val)
		}
	}

	require.NoError(t, sc.Err())

	return out
}

func findFixtureSection(secs []fixtureSection, secType, name string) *fixtureSection {
	for i := range secs {
		if secs[i].Type != secType {
			continue
		}

		if name == "" && secs[i].Name == "" {
			return &secs[i]
		}

		if secs[i].Name == name {
			return &secs[i]
		}
	}

	return nil
}

func findFixtureSectionByOption(secs []fixtureSection, secType, option, value string) *fixtureSection {
	for i := range secs {
		if secs[i].Type != secType {
			continue
		}

		for _, v := range secs[i].Options[option] {
			if v == value {
				return &secs[i]
			}
		}
	}

	return nil
}

// assertTreeMatchesFixture asserts every fixture option (values and
// list order included) matches the staged tree, minus ignoreOptions.
// It does not detect options the tree carries that the fixture lacks;
// callers that need to prove an option is absent must assert that explicitly.
func assertTreeMatchesFixture(t *testing.T, tr *uciTree, config, treeSection string, fx *fixtureSection, ignoreOptions ...string) {
	t.Helper()

	require.NotNil(t, fx, "fixture section missing")

	ignored := make(map[string]bool, len(ignoreOptions))
	for _, o := range ignoreOptions {
		ignored[o] = true
	}

	for opt, want := range fx.Options {
		if ignored[opt] {
			continue
		}

		got := tr.get(config, treeSection, opt)
		assert.Equal(t, want, got, "%s.%s.%s", config, treeSection, opt)
	}
}

// deepCopyReaderData snapshots a fakeConfigReader's option data as a
// plain nested map so two points in time can be compared with
// assert.Equal without aliasing the reader's live (mutable) storage.
func deepCopyReaderData(r *fakeConfigReader) map[string]map[string]map[string][]string {
	out := make(map[string]map[string]map[string][]string, len(r.data))

	for config, sections := range r.data {
		outSections := make(map[string]map[string][]string, len(sections))

		for section, options := range sections {
			outOptions := make(map[string][]string, len(options))

			for option, values := range options {
				outOptions[option] = append([]string(nil), values...)
			}

			outSections[section] = outOptions
		}

		out[config] = outSections
	}

	return out
}

func TestFixtureParser_GateNetwork(t *testing.T) {
	secs := loadFixture(t, "mesh-gate-router-eth", "network")

	lan := findFixtureSection(secs, "interface", "lan")
	require.NotNil(t, lan)
	assert.Equal(t, []string{"eth0"}, lan.Options["device"])

	bat0 := findFixtureSection(secs, "interface", "bat0")
	require.NotNil(t, bat0)
	assert.Equal(t, []string{"0"}, bat0.Options["multicast_mode"])

	bridge := findFixtureSectionByOption(secs, "device", "name", "br-ahwlan")
	require.NotNil(t, bridge)
	assert.Equal(t, []string{"eth1", "bat0"}, bridge.Options["ports"])
}
