package sysupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// stagedFilename is the canonical on-disk name used for an uploaded
// image. The original filename is preserved as metadata; the on-disk
// path is fixed so the slot is unambiguous and discard is a single
// unlink.
const stagedFilename = "staged-firmware.bin"

// stagedPartialFilename is the temp name used while bytes are still
// streaming in. Renamed atomically to stagedFilename on success.
const stagedPartialFilename = stagedFilename + ".partial"

// MaxStagedImageBytes caps the size of an uploaded firmware image. The
// daemon refuses uploads that would exceed this size to avoid filling
// the tmpfs-backed download directory.
const MaxStagedImageBytes int64 = 512 * 1024 * 1024

// Sentinel errors for the staged-image lifecycle.
var (
	// ErrUploadInFlight is returned when StoreStagedImage is called
	// while a previous upload is still streaming.
	ErrUploadInFlight = errors.New("sysupgrade: an upload is already in progress")

	// ErrNoStagedImage is returned when StartLocalUpgrade or
	// DiscardStagedImage is called and no image is staged.
	ErrNoStagedImage = errors.New("sysupgrade: no staged image")

	// ErrStagedPreflightFailed is returned when StartLocalUpgrade is
	// called against a staged image whose preflight check failed and
	// the operator did not pass skipPreflight.
	ErrStagedPreflightFailed = errors.New("sysupgrade: staged image failed preflight; pass skip_preflight to override")

	// ErrUploadTooLarge is returned when an upload would exceed
	// MaxStagedImageBytes.
	ErrUploadTooLarge = errors.New("sysupgrade: upload exceeds maximum allowed size")

	// ErrUploadEmpty is returned when an upload yields zero bytes.
	ErrUploadEmpty = errors.New("sysupgrade: uploaded image is empty")
)

// StoreStagedImage drains src into the daemon's staged-image slot,
// computing the sha256 inline. The returned StagedImage carries the
// finalized metadata, including the result of the filename heuristic
// match against the local hardware target and the synchronous
// "sysupgrade -T" preflight.
//
// Uploads are exclusive: a second concurrent call returns
// ErrUploadInFlight. The caller is responsible for closing src; on any
// error before finalize, the partial file is removed before returning.
func (m *Manager) StoreStagedImage(ctx context.Context, src io.Reader, filename string) (*StagedImage, error) {
	m.mu.Lock()

	if m.uploadInFlight {
		m.mu.Unlock()

		return nil, ErrUploadInFlight
	}

	m.uploadInFlight = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.uploadInFlight = false
		m.mu.Unlock()
	}()

	if err := os.MkdirAll(m.downloadDir, 0o755); err != nil {
		return nil, fmt.Errorf("sysupgrade: mkdir staged dir: %w", err)
	}

	partialPath := filepath.Join(m.downloadDir, stagedPartialFilename)
	finalPath := filepath.Join(m.downloadDir, stagedFilename)

	// Best-effort cleanup of any leftover partial from a previous
	// crashed upload before we open the new one.
	_ = os.Remove(partialPath)

	f, err := os.OpenFile(partialPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sysupgrade: create staged partial: %w", err)
	}

	hasher := sha256.New()
	limited := io.LimitReader(src, MaxStagedImageBytes+1)
	tee := io.TeeReader(limited, hasher)

	written, copyErr := io.Copy(f, tee)
	if syncErr := f.Sync(); syncErr != nil && copyErr == nil {
		copyErr = syncErr
	}

	if closeErr := f.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}

	if copyErr != nil {
		_ = os.Remove(partialPath)

		if errors.Is(copyErr, context.Canceled) || errors.Is(copyErr, context.DeadlineExceeded) {
			return nil, copyErr
		}

		return nil, fmt.Errorf("sysupgrade: write staged image: %w", copyErr)
	}

	if written == 0 {
		_ = os.Remove(partialPath)

		return nil, ErrUploadEmpty
	}

	if written > MaxStagedImageBytes {
		_ = os.Remove(partialPath)

		return nil, ErrUploadTooLarge
	}

	if err := os.Rename(partialPath, finalPath); err != nil {
		_ = os.Remove(partialPath)

		return nil, fmt.Errorf("sysupgrade: finalize staged image: %w", err)
	}

	digest := hex.EncodeToString(hasher.Sum(nil))

	deviceCompat := m.readDeviceCompatString()

	staged := &StagedImage{
		Path:         finalPath,
		Filename:     cleanUploadedFilename(filename),
		SizeBytes:    written,
		Sha256:       digest,
		UploadedAt:   time.Now().UTC(),
		DeviceCompat: deviceCompat,
	}

	if meta, metaErr := ParseImageMetadata(finalPath); metaErr == nil {
		staged.MetadataPresent = true
		staged.CompatVersion = meta.CompatVersion
		staged.CompatMessage = meta.CompatMessage
		staged.SupportedDevices = meta.EffectiveSupportedDevices()
		staged.ImageCompatible = meta.MatchesDevice(deviceCompat)
	} else if !errors.Is(metaErr, ErrNoImageMetadata) {
		m.log.Warn().Err(metaErr).Str("path", finalPath).Msg("sysupgrade: parse FWx0 metadata failed")
	}

	preflightErr := m.runner.Preflight(ctx, finalPath)
	staged.PreflightOK = preflightErr == nil

	if preflightErr != nil {
		staged.PreflightError = preflightErr.Error()

		m.log.Warn().
			Str("filename", staged.Filename).
			Str("path", finalPath).
			Err(preflightErr).
			Msg("sysupgrade: staged-image preflight failed")
	} else {
		m.log.Info().
			Str("filename", staged.Filename).
			Int64("size_bytes", written).
			Str("sha256", digest).
			Bool("metadata_present", staged.MetadataPresent).
			Bool("image_compatible", staged.ImageCompatible).
			Strs("supported_devices", staged.SupportedDevices).
			Str("device_compat", staged.DeviceCompat).
			Msg("sysupgrade: staged image accepted")
	}

	m.mu.Lock()
	m.staged = staged
	m.mu.Unlock()

	return staged.copy(), nil
}

// GetStagedImage returns a copy of the current staged image, or nil
// when no image is staged.
func (m *Manager) GetStagedImage() *StagedImage {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.staged == nil {
		return nil
	}

	return m.staged.copy()
}

// DiscardStagedImage deletes the on-disk file and clears the staged
// metadata. Returns ErrNoStagedImage when no image is staged.
func (m *Manager) DiscardStagedImage() error {
	m.mu.Lock()
	staged := m.staged
	m.staged = nil
	m.mu.Unlock()

	if staged == nil {
		return ErrNoStagedImage
	}

	if err := os.Remove(staged.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.log.Warn().Err(err).Str("path", staged.Path).Msg("sysupgrade: discard staged image: remove failed")

		return fmt.Errorf("sysupgrade: discard staged image: %w", err)
	}

	m.log.Info().Str("filename", staged.Filename).Msg("sysupgrade: staged image discarded")

	return nil
}

// StartLocalUpgrade begins the asynchronous Verify → Exec sequence
// against the currently-staged image. The Download phase is skipped.
// Returns immediately once the goroutine is spawned; later errors
// arrive on the progress stream.
func (m *Manager) StartLocalUpgrade(ctx context.Context, opts SysupgradeOptions, skipPreflight, forceUnknown bool) error {
	if _, err := opts.ToArgs(); err != nil {
		return err
	}

	currentVersion := m.currentOpenmanetVersion()
	if currentVersion.IsZero() && !forceUnknown {
		return ErrUnknownCurrentVersion
	}

	m.mu.Lock()

	if m.staged == nil {
		m.mu.Unlock()

		return ErrNoStagedImage
	}

	if !m.staged.PreflightOK && !skipPreflight {
		m.mu.Unlock()

		return ErrStagedPreflightFailed
	}

	if m.progress.Phase == PhaseDownloading || m.progress.Phase == PhaseVerifying || m.progress.Phase == PhaseUpgrading {
		m.mu.Unlock()

		return ErrBusy
	}

	staged := m.staged.copy()

	upgradeCtx, cancel := context.WithCancel(context.Background())
	_ = ctx // request ctx is observed only synchronously; the goroutine has its own lifetime.
	m.upgradeCancel = cancel
	m.upgradeStarted = false
	m.mu.Unlock()

	m.wg.Go(func() {
		m.runLocalUpgrade(upgradeCtx, staged, opts)
	})

	return nil
}

// runLocalUpgrade is the goroutine body for StartLocalUpgrade. The
// staged image is already on disk and (assuming preflight didn't fail
// or was skipped) ready to flash.
func (m *Manager) runLocalUpgrade(ctx context.Context, staged *StagedImage, opts SysupgradeOptions) {
	defer func() {
		m.mu.Lock()
		m.upgradeCancel = nil
		m.mu.Unlock()
	}()

	m.setProgress(Progress{
		Phase:      PhaseVerifying,
		AssetName:  staged.Filename,
		BytesTotal: staged.SizeBytes,
		BytesDone:  staged.SizeBytes,
		Percent:    100,
		Message:    "using uploaded image; sha256: " + staged.Sha256,
		UpdatedAt:  time.Now(),
	})

	m.setProgress(Progress{
		Phase:      PhaseReady,
		AssetName:  staged.Filename,
		BytesTotal: staged.SizeBytes,
		Message:    "launching sysupgrade against staged image",
		UpdatedAt:  time.Now(),
	})

	if mkErr := os.MkdirAll(m.downloadDir, 0o755); mkErr != nil {
		m.setPhase(PhaseFailed, "mkdir downloadDir failed", mkErr.Error())

		return
	}

	logPath := filepath.Join(m.downloadDir, stagedFilename+".log")

	m.log.Info().
		Str("image", staged.Path).
		Str("log", logPath).
		Str("filename", staged.Filename).
		Msg("sysupgrade: launching detached child for staged image")

	pid, err := m.runner.Run(ctx, staged.Path, logPath, opts)
	if err != nil {
		m.log.Error().Err(err).Str("image", staged.Path).Msg("sysupgrade: runner failed to launch")
		m.setPhase(PhaseFailed, "sysupgrade runner failed", err.Error())

		return
	}

	m.mu.Lock()
	m.upgradeStarted = true
	m.mu.Unlock()

	m.log.Info().Int("pid", pid).Str("log", logPath).Msg("sysupgrade: detached child launched")

	m.setProgress(Progress{
		Phase:     PhaseUpgrading,
		AssetName: staged.Filename,
		ChildPID:  int32(pid),
		Message:   "sysupgrade running in detached child",
		UpdatedAt: time.Now(),
	})

	m.wg.Go(func() {
		m.watchSysupgradeChild(ctx, pid, logPath, staged.Filename)
	})
}

// cleanUploadedFilename strips any path component the uploader may have
// included and returns just the basename. Empty inputs yield
// "uploaded-image.bin" so the slot always has a recognizable label.
func cleanUploadedFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "uploaded-image.bin"
	}

	// Treat both "/" and "\" as separators so a Windows-supplied path
	// is reduced to its leaf as well.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}

	if name == "" {
		return "uploaded-image.bin"
	}

	return name
}

// copy returns a defensive copy of s. The package-internal slot is
// never exposed by reference so consumers can't mutate manager state.
func (s *StagedImage) copy() *StagedImage {
	if s == nil {
		return nil
	}

	out := *s

	return &out
}
