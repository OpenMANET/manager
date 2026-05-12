package sysupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// downloadBufSize is the chunk size used when streaming an image to disk.
// Matches the precedent set by internal/frontend/whisper.go.
const downloadBufSize = 32 << 10 // 32 KiB

// progressEmitInterval rate-limits Progress emissions during a download.
const progressEmitInterval = 100 * time.Millisecond

// freeSpaceMargin is the safety factor applied to asset.SizeBytes when
// preflighting available disk space. 1.1 ensures a 10% headroom over
// the raw payload.
const freeSpaceMargin = 11

// ErrInsufficientSpace is returned by the download path when the
// destination filesystem does not have enough free bytes.
var ErrInsufficientSpace = errors.New("sysupgrade: insufficient free space for download")

// downloadOptions bundles the inputs to streamDownloadVerify so the
// signature stays manageable.
type downloadOptions struct {
	HTTP         *http.Client
	StatfsFunc   func(path string, buf *unix.Statfs_t) error
	ProgressSink func(p Progress)
	DestPath     string
	ExpectedHex  string
	Asset        Asset
}

// streamDownloadVerify performs the full preflight → fetch sums → stream
// → verify sequence. ctx cancellation aborts the in-flight HTTP request.
// On any error, the partial output file is removed so retries start
// clean. Returns the on-disk path of the verified image on success.
func streamDownloadVerify(ctx context.Context, opt downloadOptions) (string, error) {
	if err := preflightSpace(opt.DestPath, opt.Asset.SizeBytes, opt.StatfsFunc); err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(opt.DestPath), 0o755); err != nil {
		return "", fmt.Errorf("sysupgrade: mkdir destdir: %w", err)
	}

	httpClient := opt.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opt.Asset.DownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("sysupgrade: build download request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sysupgrade: do download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sysupgrade: download status %d", resp.StatusCode)
	}

	out, err := os.Create(opt.DestPath)
	if err != nil {
		return "", fmt.Errorf("sysupgrade: create destfile: %w", err)
	}

	hasher := sha256.New()
	mw := io.MultiWriter(out, hasher)

	total := opt.Asset.SizeBytes
	if total <= 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}

	downloaded, copyErr := copyWithProgress(ctx, mw, resp.Body, opt, total)
	if copyErr != nil {
		out.Close()
		os.Remove(opt.DestPath)

		return "", copyErr
	}

	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(opt.DestPath)

		return "", fmt.Errorf("sysupgrade: fsync: %w", err)
	}

	if err := out.Close(); err != nil {
		os.Remove(opt.DestPath)

		return "", fmt.Errorf("sysupgrade: close: %w", err)
	}

	// Final 100% emit so subscribers see the terminal state.
	if opt.ProgressSink != nil {
		emitDownloadProgress(opt.ProgressSink, opt.Asset.Name, downloaded, total)
	}

	if opt.ExpectedHex != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if err := VerifyDigest(opt.ExpectedHex, got); err != nil {
			os.Remove(opt.DestPath)

			return "", err
		}
	}

	return opt.DestPath, nil
}

// copyWithProgress streams src into dst in chunks, emitting rate-limited
// Progress events through opt.ProgressSink. Returns the total number of
// bytes written and any error from the I/O loop.
func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, opt downloadOptions, total int64) (int64, error) {
	buf := make([]byte, downloadBufSize)

	var (
		downloaded int64
		lastEmit   time.Time
	)

	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return downloaded, fmt.Errorf("sysupgrade: download canceled: %w", ctxErr)
		}

		n, readErr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return downloaded, fmt.Errorf("sysupgrade: write: %w", werr)
			}

			downloaded += int64(n)

			if opt.ProgressSink != nil && time.Since(lastEmit) >= progressEmitInterval {
				lastEmit = time.Now()

				emitDownloadProgress(opt.ProgressSink, opt.Asset.Name, downloaded, total)
			}
		}

		if errors.Is(readErr, io.EOF) {
			return downloaded, nil
		}

		if readErr != nil {
			return downloaded, fmt.Errorf("sysupgrade: read: %w", readErr)
		}
	}
}

// emitDownloadProgress publishes a snapshot Progress event for the
// supplied counters.
func emitDownloadProgress(sink func(Progress), assetName string, done, total int64) {
	var pct int32

	if total > 0 {
		v := min(done*100/total, 100)
		pct = int32(v)
	}

	sink(Progress{
		Phase:      PhaseDownloading,
		Percent:    pct,
		BytesDone:  done,
		BytesTotal: total,
		AssetName:  assetName,
		UpdatedAt:  time.Now(),
	})
}

// preflightSpace ensures the filesystem hosting destPath has free bytes
// at least equal to wantBytes * 1.1. statfsFunc may be nil; the real
// unix.Statfs call is used when it is.
func preflightSpace(destPath string, wantBytes int64, statfsFunc func(string, *unix.Statfs_t) error) error {
	if wantBytes <= 0 {
		return nil
	}

	if statfsFunc == nil {
		statfsFunc = unix.Statfs
	}

	dir := filepath.Dir(destPath)
	// Ensure parent exists so Statfs returns the right filesystem.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sysupgrade: mkdir destdir: %w", err)
	}

	var st unix.Statfs_t
	if err := statfsFunc(dir, &st); err != nil {
		return fmt.Errorf("sysupgrade: statfs %s: %w", dir, err)
	}

	free := int64(st.Bavail) * int64(st.Bsize) //nolint:unconvert

	required := wantBytes * freeSpaceMargin / 10
	if free < required {
		return fmt.Errorf("%w: free=%d required=%d", ErrInsufficientSpace, free, required)
	}

	return nil
}
