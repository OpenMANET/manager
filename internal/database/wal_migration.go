package database

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/rs/zerolog"
)

// hasWALHeader reports whether the SQLite database at path is stored in
// WAL journal format. It reads bytes 18 and 19 of the SQLite header —
// both == 0x02 marks the database as WAL, 0x01 marks the default
// rollback (DELETE) journal. Missing / short / unreadable files
// return false without an error so callers can treat them as a
// "nothing to migrate" state.
func hasWALHeader(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}

	defer f.Close() //nolint:errcheck

	var hdr [20]byte

	n, err := io.ReadFull(f, hdr[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read %s header: %w", path, err)
	}

	if n < 20 {
		return false, nil
	}

	return hdr[18] == 2 && hdr[19] == 2, nil
}

// removeWALDBFiles best-effort deletes the SQLite database plus its
// -wal and -shm sidecars. Called after hasWALHeader confirms the DB
// was left in WAL format by a previous VFS. Missing sidecars are not
// an error; if the .db itself can't be removed the next sql.Open
// will surface the real problem.
func removeWALDBFiles(log zerolog.Logger, dbFilePath string) {
	log.Warn().Str("path", dbFilePath).Msg(
		"Removing stale WAL-format database; will be recreated in rollback-journal mode")

	_ = os.Remove(dbFilePath)
	_ = os.Remove(dbFilePath + "-wal")
	_ = os.Remove(dbFilePath + "-shm")
}
