// Package morseregdb parses the Morse Micro regulatory database CSV
// shipped with the HaLow firmware at /usr/share/morse-regdb/channels.csv
// and exposes the legal HaLow channels per (country, bandwidth).
//
// The CSV's authoritative columns for our purposes are:
//
//	country_code, bw, s1g_chan, country
//
// Other columns (operating class, center frequency, tx power, duty
// cycle, etc.) are parsed but only the four above feed the wizard.
package morseregdb

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

// DefaultPath is where the Morse Micro userspace package installs the
// regulatory CSV on OpenMANET firmware.
const DefaultPath = "/usr/share/morse-regdb/channels.csv"

// Country is one regulatory domain entry: its ISO/region code, display
// name, and legal channels per bandwidth.
type Country struct {
	Code       string              // e.g. "US"
	Name       string              // e.g. "USA"
	Bandwidths []BandwidthChannels // sorted by Mhz ascending
}

// BandwidthChannels lists the legal channel numbers at one bandwidth.
type BandwidthChannels struct {
	Channels []uint32
	Mhz      uint32
}

// DB is an in-memory view of the regdb.
type DB struct {
	// byCode maps the country code (uppercase) to its Country entry.
	byCode map[string]*Country
	// codes is the sorted list of country codes in insertion order
	// (alphabetical after Load), used for stable iteration.
	codes []string
}

// Load reads the regulatory CSV at `path` and returns a parsed DB. If
// the file does not exist, returns ErrNotInstalled so the caller can
// distinguish "regdb missing" from a malformed file.
func Load(path string) (*DB, error) {
	f, err := os.Open(path) //nolint:gosec // path is operator-controlled, regdb is read-only
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotInstalled
		}

		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only file

	return Parse(f)
}

// ErrNotInstalled is returned by Load when the regdb CSV is absent. The
// caller is expected to treat this as a soft failure — the wizard falls
// back to a baked-in default rather than rejecting the request.
var ErrNotInstalled = errors.New("morseregdb: channels.csv not found")

// Parse reads CSV-formatted regdb data from r.
func Parse(r io.Reader) (*DB, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // tolerate occasional mismatched rows

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	cols, err := indexColumns(header)
	if err != nil {
		return nil, err
	}

	db := &DB{byCode: make(map[string]*Country, 64)}

	// channelSeen lets us deduplicate (country, bw, channel) tuples
	// without keeping a parallel set per Country.
	type seenKey struct {
		code string
		bw   uint32
		ch   uint32
	}

	channelSeen := make(map[seenKey]struct{}, 4096)

	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}

		if readErr != nil {
			return nil, fmt.Errorf("read row: %w", readErr)
		}

		if len(row) <= cols.maxIndex() {
			continue // truncated row, skip
		}

		code := strings.ToUpper(strings.TrimSpace(row[cols.code]))
		name := strings.TrimSpace(row[cols.name])

		bw, err := strconv.ParseUint(strings.TrimSpace(row[cols.bw]), 10, 32)
		if err != nil {
			continue // malformed bw — skip the row, don't fail the whole file
		}

		ch, err := strconv.ParseUint(strings.TrimSpace(row[cols.channel]), 10, 32)
		if err != nil {
			continue
		}

		if code == "" {
			continue
		}

		k := seenKey{code: code, bw: uint32(bw), ch: uint32(ch)}
		if _, dup := channelSeen[k]; dup {
			continue
		}

		channelSeen[k] = struct{}{}

		c := db.byCode[code]
		if c == nil {
			c = &Country{Code: code, Name: name}
			db.byCode[code] = c
		}

		// Country.Name may be empty in early rows for a code; keep the
		// first non-empty name we see.
		if c.Name == "" && name != "" {
			c.Name = name
		}

		c.appendChannel(uint32(bw), uint32(ch))
	}

	// Sort each country's bandwidth+channel lists for deterministic output.
	for _, c := range db.byCode {
		c.normalize()
	}

	db.codes = make([]string, 0, len(db.byCode))
	for code := range db.byCode {
		db.codes = append(db.codes, code)
	}

	slices.Sort(db.codes)

	return db, nil
}

// Countries returns every regulatory domain in the DB, sorted by code.
// The returned slice is owned by the caller (safe to mutate).
func (d *DB) Countries() []Country {
	out := make([]Country, 0, len(d.codes))

	for _, code := range d.codes {
		c := d.byCode[code]
		if c == nil {
			continue
		}

		out = append(out, *c.clone())
	}

	return out
}

// Country returns the entry for `code` (case-insensitive) or nil.
func (d *DB) Country(code string) *Country {
	c, ok := d.byCode[strings.ToUpper(code)]
	if !ok {
		return nil
	}

	return c.clone()
}

// IsLegalChannel reports whether (country, bandwidthMhz, channel) is
// present in the regdb. Used by handler validation to reject illegal
// combinations before we ever touch UCI.
func (d *DB) IsLegalChannel(country string, bandwidthMhz, channel uint32) bool {
	c, ok := d.byCode[strings.ToUpper(country)]
	if !ok {
		return false
	}

	for _, bw := range c.Bandwidths {
		if bw.Mhz != bandwidthMhz {
			continue
		}

		return slices.Contains(bw.Channels, channel)
	}

	return false
}

// columnIndices captures the column offsets we care about in the CSV.
// indexColumns extracts them from the header row so a future column
// reorder (or addition) doesn't break us silently.
type columnIndices struct {
	code    int
	bw      int
	channel int
	name    int
}

func (c columnIndices) maxIndex() int {
	m := c.code
	for _, v := range []int{c.bw, c.channel, c.name} {
		if v > m {
			m = v
		}
	}

	return m
}

func indexColumns(header []string) (columnIndices, error) {
	idx := columnIndices{code: -1, bw: -1, channel: -1, name: -1}

	for i, name := range header {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "country_code":
			idx.code = i
		case "bw":
			idx.bw = i
		case "s1g_chan":
			idx.channel = i
		case "country":
			idx.name = i
		}
	}

	for col, i := range map[string]int{
		"country_code": idx.code,
		"bw":           idx.bw,
		"s1g_chan":     idx.channel,
		"country":      idx.name,
	} {
		if i < 0 {
			return idx, fmt.Errorf("regdb: header missing required column %q", col)
		}
	}

	return idx, nil
}

func (c *Country) appendChannel(bw, ch uint32) {
	for i := range c.Bandwidths {
		if c.Bandwidths[i].Mhz == bw {
			c.Bandwidths[i].Channels = append(c.Bandwidths[i].Channels, ch)

			return
		}
	}

	c.Bandwidths = append(c.Bandwidths, BandwidthChannels{
		Mhz:      bw,
		Channels: []uint32{ch},
	})
}

func (c *Country) normalize() {
	slices.SortFunc(c.Bandwidths, func(a, b BandwidthChannels) int {
		return int(a.Mhz) - int(b.Mhz)
	})

	for i := range c.Bandwidths {
		slices.Sort(c.Bandwidths[i].Channels)
	}
}

func (c *Country) clone() *Country {
	out := &Country{
		Code:       c.Code,
		Name:       c.Name,
		Bandwidths: make([]BandwidthChannels, len(c.Bandwidths)),
	}

	for i, bw := range c.Bandwidths {
		channels := make([]uint32, len(bw.Channels))
		copy(channels, bw.Channels)
		out.Bandwidths[i] = BandwidthChannels{Mhz: bw.Mhz, Channels: channels}
	}

	return out
}
