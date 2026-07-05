package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

// sqliteHeader returns a minimal 20-byte SQLite header with the given
// write/read journal-format bytes at offsets 18 and 19. Every other
// byte follows the canonical SQLite defaults so hasWALHeader only
// keys on the journal-format bytes we're actually testing.
func sqliteHeader(writeVersion, readVersion byte) []byte {
	hdr := make([]byte, 20)
	copy(hdr, []byte("SQLite format 3\x00"))
	hdr[16], hdr[17] = 0x10, 0x00 // page size = 4096 (big-endian)
	hdr[18] = writeVersion
	hdr[19] = readVersion

	return hdr
}

func TestHasWALHeader_MissingFileReturnsFalse(t *testing.T) {
	ok, err := hasWALHeader(filepath.Join(t.TempDir(), "does-not-exist.db"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if ok {
		t.Fatal("expected false for missing file, got true")
	}
}

func TestHasWALHeader_RollbackJournalReturnsFalse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rollback.db")
	if err := os.WriteFile(p, sqliteHeader(0x01, 0x01), 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := hasWALHeader(p)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if ok {
		t.Fatal("expected false for rollback-format DB, got true")
	}
}

func TestHasWALHeader_WALFormatReturnsTrue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "wal.db")
	if err := os.WriteFile(p, sqliteHeader(0x02, 0x02), 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := hasWALHeader(p)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if !ok {
		t.Fatal("expected true for WAL-format DB, got false")
	}
}

func TestHasWALHeader_ShortFileReturnsFalse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "short.db")
	if err := os.WriteFile(p, []byte("SQLite"), 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := hasWALHeader(p)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if ok {
		t.Fatal("expected false for too-short file, got true")
	}
}

func TestRemoveWALDBFiles_DeletesDBAndSidecars(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "wal.db")

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(p+suffix, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup %s: %v", suffix, err)
		}
	}

	removeWALDBFiles(zerolog.Nop(), p)

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(p + suffix); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err=%v", p+suffix, err)
		}
	}
}

func TestRemoveWALDBFiles_MissingSidecarsAreIgnored(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "wal.db")

	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// -wal and -shm never existed; removeWALDBFiles must not panic or
	// treat the missing sidecars as an error.
	removeWALDBFiles(zerolog.Nop(), p)

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("expected .db to be removed, stat err=%v", err)
	}
}
