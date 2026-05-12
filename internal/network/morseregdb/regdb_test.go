package morseregdb_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/openmanet/openmanetd/internal/network/morseregdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixturePath locates testfixtures/setup-wizard/channels.csv relative to
// the module root. Walk up from this file with runtime.Caller per the
// project's testing conventions (no hardcoded absolute paths).
func fixturePath(t *testing.T) string {
	t.Helper()

	_, here, _, ok := runtime.Caller(0)
	require.True(t, ok)

	// here = .../internal/network/morseregdb/regdb_test.go — walk four
	// dirs up to the module root.
	root := here
	for range 4 {
		root = filepath.Dir(root)
	}

	return filepath.Join(root, "testfixtures", "setup-wizard", "channels.csv")
}

func TestLoad_FixtureContainsExpectedCountries(t *testing.T) {
	db, err := morseregdb.Load(fixturePath(t))
	require.NoError(t, err)

	countries := db.Countries()
	require.NotEmpty(t, countries)

	codes := make([]string, 0, len(countries))
	for _, c := range countries {
		codes = append(codes, c.Code)
	}

	// The fixture is a snapshot of the Morse Micro regdb that ships
	// with OpenMANET firmware. Spot-check a handful of expected
	// regulatory domains.
	assert.Contains(t, codes, "US")
	assert.Contains(t, codes, "GB")
	assert.Contains(t, codes, "JP")
	assert.Contains(t, codes, "AU")
}

func TestLoad_USAHasAllFourBandwidths(t *testing.T) {
	db, err := morseregdb.Load(fixturePath(t))
	require.NoError(t, err)

	us := db.Country("US")
	require.NotNil(t, us)
	assert.Equal(t, "USA", us.Name)

	bws := make(map[uint32]bool, 4)
	for _, b := range us.Bandwidths {
		bws[b.Mhz] = true
	}

	for _, want := range []uint32{1, 2, 4, 8} {
		assert.True(t, bws[want], "US must include %d MHz", want)
	}
}

func TestLoad_USA8MHzChannelsMatchSpec(t *testing.T) {
	// The Morse driver in US allocates 8 MHz to channels 12, 28, 44 only.
	// If this regresses, the wizard's channel dropdown will be wrong.
	db, err := morseregdb.Load(fixturePath(t))
	require.NoError(t, err)

	us := db.Country("US")
	require.NotNil(t, us)

	var channels []uint32

	for _, b := range us.Bandwidths {
		if b.Mhz == 8 {
			channels = b.Channels

			break
		}
	}

	assert.Equal(t, []uint32{12, 28, 44}, channels)
}

func TestIsLegalChannel(t *testing.T) {
	db, err := morseregdb.Load(fixturePath(t))
	require.NoError(t, err)

	// US: ch 42 is legal at 2 MHz, illegal at 1 MHz.
	assert.True(t, db.IsLegalChannel("US", 2, 42))
	assert.False(t, db.IsLegalChannel("US", 1, 42))

	// US: ch 12 is legal at 8 MHz only.
	assert.True(t, db.IsLegalChannel("US", 8, 12))
	assert.False(t, db.IsLegalChannel("US", 4, 12))

	// Unknown country.
	assert.False(t, db.IsLegalChannel("ZZ", 2, 6))

	// Case-insensitive country lookup.
	assert.True(t, db.IsLegalChannel("us", 2, 42))
}

func TestLoad_MissingFileReturnsErrNotInstalled(t *testing.T) {
	_, err := morseregdb.Load(filepath.Join(t.TempDir(), "nope.csv"))
	require.ErrorIs(t, err, morseregdb.ErrNotInstalled)
}

func TestParse_HeaderMissingRequiredColumn(t *testing.T) {
	// "bw" omitted on purpose.
	csv := "country_code,s1g_chan,country\nUS,42,USA\n"

	_, err := morseregdb.Parse(strings.NewReader(csv))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing required column "bw"`)
}

func TestParse_MalformedRowsAreSkipped(t *testing.T) {
	// Row 2 has a non-numeric bandwidth; it should be skipped without
	// failing the whole parse.
	csv := "country_code,bw,s1g_chan,country\n" +
		"US,1,1,USA\n" +
		"US,foo,3,USA\n" +
		"US,2,42,USA\n"

	db, err := morseregdb.Parse(strings.NewReader(csv))
	require.NoError(t, err)

	us := db.Country("US")
	require.NotNil(t, us)

	// Two valid rows produced two channel allocations.
	total := 0
	for _, b := range us.Bandwidths {
		total += len(b.Channels)
	}

	assert.Equal(t, 2, total)
}

func TestParse_ChannelsAreDeduplicated(t *testing.T) {
	csv := "country_code,bw,s1g_chan,country\n" +
		"US,2,42,USA\n" +
		"US,2,42,USA\n" + // exact duplicate
		"US,2,46,USA\n"

	db, err := morseregdb.Parse(strings.NewReader(csv))
	require.NoError(t, err)

	us := db.Country("US")
	require.NotNil(t, us)
	require.Len(t, us.Bandwidths, 1)
	assert.Equal(t, []uint32{42, 46}, us.Bandwidths[0].Channels)
}

func TestCountriesReturnsCloneNotInternalState(t *testing.T) {
	csv := "country_code,bw,s1g_chan,country\nUS,2,42,USA\n"

	db, err := morseregdb.Parse(strings.NewReader(csv))
	require.NoError(t, err)

	first := db.Countries()
	require.Len(t, first, 1)
	require.Len(t, first[0].Bandwidths, 1)

	first[0].Bandwidths[0].Channels[0] = 999

	second := db.Countries()
	assert.Equal(t, uint32(42), second[0].Bandwidths[0].Channels[0],
		"mutating the result of Countries() must not affect internal state")
}
