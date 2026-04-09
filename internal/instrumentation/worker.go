package instrumentation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog"
)

// DefaultInterval is the fallback capture period used when WorkerOptions
// leaves Interval at zero. Chosen conservatively so the default does not
// produce large volumes of snapshot files on /tmp.
const DefaultInterval = 60 * time.Second

// WorkerOptions configures a periodic snapshot writer.
type WorkerOptions struct {
	// Registry supplies captures. Required.
	Registry *Registry
	// Interval is the capture period. Values <= 0 fall back to DefaultInterval.
	Interval time.Duration
	// OutputDir is the filesystem directory that snapshot files are written
	// into. Required. The directory must exist and be writable.
	OutputDir string
	// FilenamePrefix is the prefix used for generated snapshot filenames.
	// The worker appends a high-resolution timestamp and the ".json"
	// extension. Empty defaults to "snapshot".
	FilenamePrefix string
	// Log is the zerolog logger the worker emits diagnostic messages on.
	// The zero value (disabled logger) is safe.
	Log zerolog.Logger
}

// Worker periodically captures snapshots and writes each one to its own
// timestamped JSON file. Lifecycle is tied to the context passed to Run.
// A Worker owns a reusable Envelope so Capture is zero-alloc after warmup.
type Worker struct {
	opts WorkerOptions
	env  Envelope
	// buf is a reusable buffer used to format snapshot file names without
	// allocating on every tick.
	nameBuf []byte
}

// NewWorker constructs a Worker ready for Run. It does not create the
// output directory — callers are expected to ensure it exists before
// Run is invoked.
func NewWorker(opts WorkerOptions) (*Worker, error) {
	if opts.Registry == nil {
		return nil, errors.New("instrumentation: worker registry is nil")
	}

	if opts.OutputDir == "" {
		return nil, errors.New("instrumentation: worker output directory is empty")
	}

	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}

	if opts.FilenamePrefix == "" {
		opts.FilenamePrefix = "snapshot"
	}

	return &Worker{
		opts:    opts,
		nameBuf: make([]byte, 0, 64),
	}, nil
}

// Run captures and writes snapshots until ctx is cancelled. It returns nil
// on graceful shutdown. Per-tick write failures are logged at Error level
// and do not abort the worker.
//
// Run is intended to be called in its own goroutine from application
// startup; it blocks for the lifetime of the worker.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.opts.Interval)
	defer ticker.Stop()

	w.opts.Log.Info().
		Dur("interval", w.opts.Interval).
		Str("output_dir", w.opts.OutputDir).
		Msg("instrumentation: snapshot worker started")

	// Emit one snapshot immediately so operators see output on the first
	// interval rather than having to wait the full period. Errors are
	// non-fatal.
	w.captureOnce()

	for {
		select {
		case <-ctx.Done():
			w.opts.Log.Info().Msg("instrumentation: snapshot worker stopping")

			return
		case <-ticker.C:
			w.captureOnce()
		}
	}
}

// captureOnce runs a single capture+write iteration. Errors are logged but
// never propagated — a stuck filesystem should not be able to wedge the
// worker.
func (w *Worker) captureOnce() {
	w.opts.Registry.Capture(&w.env)

	path, err := w.write(&w.env)
	if err != nil {
		w.opts.Log.Error().Err(err).Msg("instrumentation: snapshot write failed")

		return
	}

	w.opts.Log.Debug().Str("path", path).Msg("instrumentation: snapshot written")
}

// write marshals env to JSON and stores it under OutputDir with a
// timestamped filename. The filename format is
// "<prefix>-<unix-nanos>.json". Returns the absolute path of the written
// file.
func (w *Worker) write(env *Envelope) (string, error) {
	// Build the filename into the reusable nameBuf so no string concatenation
	// allocates on every tick. The leading slash is optional — OutputDir
	// may or may not end in one.
	w.nameBuf = w.nameBuf[:0]
	w.nameBuf = append(w.nameBuf, w.opts.FilenamePrefix...)
	w.nameBuf = append(w.nameBuf, '-')
	w.nameBuf = strconv.AppendInt(w.nameBuf, env.CapturedAtStart.UnixNano(), 10)
	w.nameBuf = append(w.nameBuf, ".json"...)

	path := w.opts.OutputDir
	if len(path) == 0 || path[len(path)-1] != os.PathSeparator {
		path += string(os.PathSeparator)
	}

	path += string(w.nameBuf)

	f, err := os.Create(path) //nolint:gosec // path is built from a config-supplied directory and a controlled prefix.
	if err != nil {
		return "", fmt.Errorf("create snapshot file: %w", err)
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	if err := enc.Encode(env); err != nil {
		_ = f.Close()

		return "", fmt.Errorf("encode snapshot: %w", err)
	}

	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close snapshot file: %w", err)
	}

	return path, nil
}
