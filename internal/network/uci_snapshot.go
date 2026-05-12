package network

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/digineo/go-uci/v2"
)

// FileSystemUCISnapshot captures the raw byte contents of a set of
// /etc/config/<name> files at a point in time. The setup wizard uses
// this to roll back UCI to its pre-apply state when a phase fails
// after commit has run.
//
// Restoring the snapshot writes each file atomically (temp file +
// rename), then asks the supplied UCI tree to reload its in-memory
// view from disk so subsequent SetType/Get calls see the restored
// state.
type FileSystemUCISnapshot struct {
	files map[string][]byte
}

// Configs returns the names of the UCI configs captured. Used by the
// wizard's snapshot-event message and the round-trip tests.
func (s *FileSystemUCISnapshot) Configs() []string {
	out := make([]string, 0, len(s.files))
	for name := range s.files {
		out = append(out, name)
	}

	return out
}

// FileSystemUCISnapshotter implements the wizard's UCISnapshotter
// contract by reading raw config files from /etc/config/. Restore
// writes them back atomically AND calls Reload on every supplied
// reader so the wizard's in-memory tree picks up the restored state.
//
// The Reloaders map associates each config name with the
// network.ConfigReader the wizard uses for that config — typically
// just one shared go-uci tree per the openmanetd wiring. After
// writing the file, Restore calls reader.ReloadConfig() on each
// distinct reader so its in-memory dirty state is discarded.
type FileSystemUCISnapshotter struct {
	Tree     uci.Tree
	TreePath string
}

// configFilePath returns the absolute path to a UCI config file.
func (s *FileSystemUCISnapshotter) configFilePath(name string) string {
	root := s.TreePath
	if root == "" {
		root = "/etc/config"
	}

	return filepath.Join(root, name)
}

// Snapshot reads the supplied configs from /etc/config and returns
// an opaque snapshot the caller can later pass to Restore. Missing
// configs are captured with empty bytes so Restore can re-create the
// "no file" state if needed (rare, but possible for optional
// configs like luci on minimal builds).
func (s *FileSystemUCISnapshotter) Snapshot(_ context.Context, configs []string) (*FileSystemUCISnapshot, error) {
	out := &FileSystemUCISnapshot{
		files: make(map[string][]byte, len(configs)),
	}

	for _, name := range configs {
		path := s.configFilePath(name)

		data, err := os.ReadFile(path) //nolint:gosec // path constructed from a fixed config root + sanitized name
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// File doesn't exist — capture as empty so Restore
				// can either re-create-then-rm or leave alone.
				out.files[name] = nil

				continue
			}

			return nil, fmt.Errorf("reading %s: %w", path, err)
		}

		out.files[name] = data
	}

	return out, nil
}

// Restore writes the snapshot's file contents back to /etc/config
// atomically (temp file + rename) and triggers an in-memory reload
// of the UCI tree so subsequent reads see the restored state.
//
// Errors writing individual files are aggregated and returned; the
// snapshotter attempts every restore even if an earlier one failed,
// to maximize the chance the device boots cleanly afterward.
func (s *FileSystemUCISnapshotter) Restore(_ context.Context, snapshot *FileSystemUCISnapshot) error {
	if snapshot == nil {
		return errors.New("nil snapshot")
	}

	var firstErr error

	for name, data := range snapshot.files {
		if err := s.writeAtomic(s.configFilePath(name), data); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("restoring %s: %w", name, err)
			}

			continue
		}

		if s.Tree != nil {
			// LoadConfig(name, true) discards in-memory dirty state
			// for `name` and re-reads from disk. Without this, the
			// wizard's in-memory tree would still hold the failed
			// apply's writes.
			if err := s.Tree.LoadConfig(name, true); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("reloading %s in tree: %w", name, err)
				}
			}
		}
	}

	return firstErr
}

// writeAtomic writes data to path via a temp file in the same
// directory followed by a rename, so a crash during the write leaves
// either the old or new contents but never a partial file.
func (s *FileSystemUCISnapshotter) writeAtomic(path string, data []byte) error {
	if data == nil {
		// Snapshot captured a non-existent file; remove the path so
		// the post-restore state matches.
		err := os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing %s: %w", path, err)
		}

		return nil
	}

	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".uci-snapshot-")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}

	tmpPath := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()

		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		cleanup()

		return fmt.Errorf("fsync temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		cleanup()

		return fmt.Errorf("closing temp file: %w", err)
	}

	// Match the source file's permissions when possible. UCI configs
	// are conventionally 0644 root:root.
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("rename %s -> %s: %w", tmpPath, path, err)
	}

	return nil
}
