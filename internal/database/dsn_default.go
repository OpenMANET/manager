//go:build !mipsle

package database

import "github.com/rs/zerolog"

// sqliteDSNSuffix is appended to the SQLite URI after the existing query
// parameters. Empty on architectures that support the default unix VFS
// (e.g. amd64, arm64 — Pi3, Pi4, Venice).
const sqliteDSNSuffix = ""

// sqliteSetupPragmas are executed in order right after open. WAL mode is
// enabled here for concurrent read performance on filesystems that
// support it (ext4 etc).
var sqliteSetupPragmas = []string{ //nolint:gochecknoglobals
	"PRAGMA journal_mode=WAL",
	"PRAGMA busy_timeout=5000",
	"PRAGMA synchronous=NORMAL",
}

// clearStaleWALDB is a no-op on architectures where WAL is supported.
// The mipsle build (dsn_mipsle.go) replaces this with a real one-time
// cleanup of WAL-format databases left over from previous openmanetd
// versions.
func clearStaleWALDB(zerolog.Logger, string) error { return nil }
