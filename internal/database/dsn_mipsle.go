//go:build mipsle

package database

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/rs/zerolog"
)

// sqliteDSNSuffix forces SQLite onto the "unix-dotfile" VFS for ramips
// targets (halowlink2 / mt7621, ht-hd01-v2 / mt76x8). The default unix
// VFS uses fcntl(F_SETLK) for file locks, which returns EINVAL on the
// overlayfs+jffs2 rootfs these boards ship with — the original failure
// surfaced as "ping database: disk I/O error: invalid argument" and
// crash-looped openmanetd.
//
// unix-dotfile uses a sibling ".lck" file as a lock sentinel (no fcntl
// at all), which is the right fit for embedded filesystems that don't
// fully implement POSIX advisory locks. WAL is incompatible with
// dotfile locking, so the rollback (DELETE) journal is used instead —
// sufficient for the small mesh_nodes table workload.
const sqliteDSNSuffix = "&vfs=unix-dotfile"

var sqliteSetupPragmas = []string{ //nolint:gochecknoglobals
	"PRAGMA busy_timeout=5000",
	"PRAGMA synchronous=NORMAL",
}

// clearStaleWALDB removes a pre-existing WAL-format database so the
// unix-dotfile VFS can recreate it in rollback-journal mode. Earlier
// openmanetd builds on these boards left the DB header marked as WAL
// (bytes 18/19 == 0x02); the unix-dotfile VFS can't open those, and
// would crash-loop with "unable to open database file: no such file or
// directory" while it hunts for the missing WAL/SHM sidecars.
//
// The DB only holds rebuildable mesh-node state, so a one-time delete
// is safe. Anything other than a clean WAL header is left alone.
func clearStaleWALDB(log zerolog.Logger, dbFilePath string) error {
	f, err := os.Open(dbFilePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var hdr [20]byte
	n, _ := io.ReadFull(f, hdr[:])
	_ = f.Close()
	if n < 20 || hdr[18] != 2 || hdr[19] != 2 {
		return nil
	}
	log.Warn().Str("path", dbFilePath).Msg("Removing stale WAL-format database; will be recreated in rollback-journal mode")
	_ = os.Remove(dbFilePath)
	_ = os.Remove(dbFilePath + "-wal")
	_ = os.Remove(dbFilePath + "-shm")
	return nil
}
