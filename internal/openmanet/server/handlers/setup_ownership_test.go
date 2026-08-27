package handlers_test

// setup_ownership_test.go pins the WIZARD's value for every fixture
// row the ownership map (fixture_test.go) marks as daemon- or
// operator-owned. Those rows are skipped by fixture parity on
// purpose; without these tests nothing would prove the wizard writes
// the bootstrap values the daemon later replaces.

import (
	"net"
	"strconv"
	"strings"
	"testing"

	setupv1 "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownershipScenarios returns the two profiles that have captured
// fixtures, keyed by the after/<scenario> directory name.
func ownershipScenarios() map[string]*setupv1.MeshNodeProfile {
	return map[string]*setupv1.MeshNodeProfile{
		"mesh-gate-router-eth": gateRouterEthProfile(),
		"mesh-point-extender":  pointExtenderProfile(),
	}
}

// TestOwnership_AhwlanPoolStartIsWizardOffset pins the wizard-side
// pool start (LuCI's 255 + 16k formula, RandomDhcpStart). The daemon
// rewrites start at reservation time, so fixture parity ignores it.
func TestOwnership_AhwlanPoolStartIsWizardOffset(t *testing.T) {
	for scenario, profile := range ownershipScenarios() {
		t.Run(scenario, func(t *testing.T) {
			tr := runScenarioApply(t, profile)

			pool := tr.findDhcpPool("ahwlan")
			require.NotEmpty(t, pool)

			raw := tr.getOne("dhcp", pool, "start")
			start, err := strconv.Atoi(raw)
			require.NoError(t, err, "dhcp.ahwlan.start %q must be an integer", raw)

			assert.GreaterOrEqual(t, start, 255)
			assert.LessOrEqual(t, start, 255+16*14)
			assert.Zero(t, (start-255)%16, "start %d must be 255 + 16k", start)
		})
	}
}

// TestOwnership_PointKeepsLanUntilReservation pins that the wizard
// still writes network.lan and dhcp.lan on mesh points (LuCI parity).
// The daemon deletes both ~125 s after boot, which is why the extender
// fixture lacks them (daemonRemovedSections).
func TestOwnership_PointKeepsLanUntilReservation(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())

	for _, row := range daemonRemovedSections() {
		assert.Truef(t, tr.hasSection(row.Config, row.SectionName),
			"wizard must write %s.%s on points; the daemon removes it later", row.Config, row.SectionName)
	}
}

// TestOwnership_GateAhwlanHasNoIPaddr pins the gate side of the
// two-stage addressing design: the wizard leaves ahwlan.ipaddr unset
// on gates and the daemon claims the first free 10.41.0.x.
func TestOwnership_GateAhwlanHasNoIPaddr(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	assert.Empty(t, tr.get("network", "ahwlan", "ipaddr"),
		"gate ahwlan.ipaddr is daemon-owned and must not be staged by the wizard")
}

// TestOwnership_MapRowsExistInFixtures guards the ownership map
// against drift: every per-option row must name a section and option
// that the fixture actually carries, otherwise the row is dead.
func TestOwnership_MapRowsExistInFixtures(t *testing.T) {
	for _, row := range ownedRows() {
		scenarios := []string{"mesh-gate-router-eth", "mesh-point-extender"}
		if row.Scenario != "" {
			scenarios = []string{row.Scenario}
		}

		for _, scenario := range scenarios {
			secs := loadFixture(t, scenario, row.Config)
			fx := findFixtureSection(secs, row.SectionType, row.SectionName)
			require.NotNilf(t, fx, "ownership row %s %s %q missing from after/%s", row.Config, row.SectionType, row.SectionName, scenario)

			if row.Option == "" {
				continue
			}

			_, ok := fx.Options[row.Option]
			assert.Truef(t, ok, "ownership row %s.%s.%s missing from after/%s", row.Config, row.SectionName, row.Option, scenario)
		}
	}
}

// ipInBootstrapWindow reports whether ip is a valid IPv4 in the
// wizard's 10.41.254.0/24 bootstrap window (RandomMeshIP).
func ipInBootstrapWindow(ip string) bool {
	return net.ParseIP(ip) != nil && strings.HasPrefix(ip, "10.41.254.")
}
