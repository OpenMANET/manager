package network

import (
	"math/rand"
	"net"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomMAC_DeterministicUnderSeed(t *testing.T) {
	r1 := rand.New(rand.NewSource(42))
	r2 := rand.New(rand.NewSource(42))

	got1 := RandomMAC(r1)
	got2 := RandomMAC(r2)

	assert.Equal(t, got1, got2, "same seed must produce same MAC")
}

func TestRandomMAC_FormatAndPrefix(t *testing.T) {
	pattern := regexp.MustCompile(`^F2:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}$`)

	r := rand.New(rand.NewSource(1))
	for i := 0; i < 100; i++ {
		mac := RandomMAC(r)
		assert.Truef(t, pattern.MatchString(mac), "got %q", mac)
	}
}

func TestRandomWifiKey_DeterministicUnderSeed(t *testing.T) {
	r1 := rand.New(rand.NewSource(123))
	r2 := rand.New(rand.NewSource(123))

	assert.Equal(t, RandomWifiKey(r1), RandomWifiKey(r2))
}

func TestRandomWifiKey_LengthAndCharset(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 100; i++ {
		key := RandomWifiKey(r)
		assert.Len(t, key, WizardWifiKeyLen, "expected exactly 8 chars")

		for _, c := range key {
			assert.Containsf(t, WizardWifiKeyCharset, string(c),
				"character %q not in wizard charset", c)
		}
	}
}

func TestRandomDhcpStart_DeterministicUnderSeed(t *testing.T) {
	r1 := rand.New(rand.NewSource(99))
	r2 := rand.New(rand.NewSource(99))

	assert.Equal(t, RandomDhcpStart(r1), RandomDhcpStart(r2))
}

func TestRandomDhcpStart_RangeAndAlignment(t *testing.T) {
	// LuCI formula: 255 + 16*Math.floor(Math.random()*15)
	// Possible values: 255, 271, 287, ..., 255+16*14 = 479. 15 distinct
	// values aligned on /28 boundaries.
	r := rand.New(rand.NewSource(3))
	seen := make(map[int]struct{})

	for i := 0; i < 1000; i++ {
		v := RandomDhcpStart(r)
		assert.GreaterOrEqual(t, v, 255)
		assert.LessOrEqual(t, v, 479)
		assert.Equal(t, 0, (v-255)%16, "value must align on /28 from 255")
		seen[v] = struct{}{}
	}

	assert.Len(t, seen, 15, "1000 samples should cover all 15 possible values")
}

func TestRandomMeshIP_DeterministicUnderSeed(t *testing.T) {
	r1 := rand.New(rand.NewSource(55))
	r2 := rand.New(rand.NewSource(55))

	got1, err := RandomMeshIP("10.41.0.0", r1)
	require.NoError(t, err)

	got2, err := RandomMeshIP("10.41.0.0", r2)
	require.NoError(t, err)

	assert.Equal(t, got1, got2)
}

func TestRandomMeshIP_FormatAndRange(t *testing.T) {
	r := rand.New(rand.NewSource(11))

	for i := 0; i < 200; i++ {
		got, err := RandomMeshIP("10.41.0.0", r)
		require.NoError(t, err)

		ip := net.ParseIP(got).To4()
		require.NotNil(t, ip, "must parse as IPv4: %q", got)

		assert.Equal(t, byte(10), ip[0])
		assert.Equal(t, byte(41), ip[1])
		assert.Equal(t, byte(254), ip[2], "third octet must be pinned to 254")
		assert.LessOrEqual(t, ip[3], byte(253), "fourth octet must be in [0,253]")
	}
}

func TestRandomMeshIP_UsesFirstTwoOctetsOfBase(t *testing.T) {
	// getRandomIpaddr ignores the third and fourth octets of the base
	// and pins the third to 254. Vary the base to confirm only the
	// first two propagate through.
	r := rand.New(rand.NewSource(0))
	got, err := RandomMeshIP("172.16.5.7", r)
	require.NoError(t, err)

	ip := net.ParseIP(got).To4()
	require.NotNil(t, ip)
	assert.Equal(t, byte(172), ip[0])
	assert.Equal(t, byte(16), ip[1])
	assert.Equal(t, byte(254), ip[2])
}

func TestRandomMeshIP_AvoidsFactoryIP(t *testing.T) {
	// Force the RNG path that would otherwise return 10.41.254.1 and
	// confirm the function rerolls. We don't have a public seed that
	// hits .1 directly, but running enough iterations should never
	// produce the factory IP because the loop rerolls on a hit.
	r := rand.New(rand.NewSource(0))

	for i := 0; i < 5000; i++ {
		got, err := RandomMeshIP("10.41.0.0", r)
		require.NoError(t, err)
		assert.NotEqual(t, FactoryMeshIP, got)
	}
}

func TestRandomMeshIP_RejectsNonIPv4(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	_, err := RandomMeshIP("not-an-ip", r)
	assert.Error(t, err)

	_, err = RandomMeshIP("2001:db8::1", r)
	assert.Error(t, err)
}
