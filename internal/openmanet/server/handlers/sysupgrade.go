package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	supbv1 "github.com/openmanet/openmanetd/internal/api/openmanet/sysupgrade/v1"
	"github.com/openmanet/openmanetd/internal/sysupgrade"
)

// SysupgradeManager is the consumer-side interface the handler depends
// on. The production implementation is *sysupgrade.Manager; tests pass
// a hand-written fake. Defined at the consumer per the project's API
// design rules.
type SysupgradeManager interface {
	GetSystemInfo(ctx context.Context) (*sysupgrade.SystemInfo, error)
	ListAvailableUpdates(ctx context.Context, force, includePre bool) ([]sysupgrade.Update, time.Time, error)
	GetReleaseDetail(ctx context.Context, tag string) (sysupgrade.Release, error)
	StartUpgrade(ctx context.Context, tag, asset string, opts sysupgrade.SysupgradeOptions, forceUnknown bool) error
	CancelUpgrade(ctx context.Context) error
	GetUpgradeStatus(ctx context.Context) sysupgrade.Progress
	Subscribe(ctx context.Context) (<-chan sysupgrade.Progress, func())
	GetStagedImage() *sysupgrade.StagedImage
	DiscardStagedImage() error
	StartLocalUpgrade(ctx context.Context, opts sysupgrade.SysupgradeOptions, skipPreflight, forceUnknown bool) error
}

// SysupgradeService implements the sysupgrade ConnectRPC service. It is
// a thin proto adapter over a SysupgradeManager.
type SysupgradeService struct {
	Log     zerolog.Logger
	Manager SysupgradeManager
}

// GetSystemInfo returns the rich system metadata.
func (s *SysupgradeService) GetSystemInfo(ctx context.Context, _ *emptypb.Empty) (*supbv1.GetSystemInfoResponse, error) {
	info, err := s.Manager.GetSystemInfo(ctx)
	if err != nil {
		s.Log.Error().Err(err).Msg("sysupgrade: GetSystemInfo failed")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get system info: %w", err))
	}

	return &supbv1.GetSystemInfoResponse{Info: systemInfoToProto(info)}, nil
}

// ListAvailableUpdates returns releases newer than current with a
// matching asset.
func (s *SysupgradeService) ListAvailableUpdates(ctx context.Context, req *supbv1.ListAvailableUpdatesRequest) (*supbv1.ListAvailableUpdatesResponse, error) {
	updates, fetchedAt, err := s.Manager.ListAvailableUpdates(ctx, req.GetForceRefresh(), req.GetIncludePrerelease())
	if err != nil {
		return nil, mapManagerError(s.Log, err, "list available updates")
	}

	out := &supbv1.ListAvailableUpdatesResponse{
		Updates: make([]*supbv1.Update, 0, len(updates)),
	}

	if !fetchedAt.IsZero() {
		out.FetchedAt = timestamppb.New(fetchedAt)
	}

	for _, u := range updates {
		out.Updates = append(out.Updates, updateToProto(u))
	}

	return out, nil
}

// GetReleaseDetail returns one release by tag.
func (s *SysupgradeService) GetReleaseDetail(ctx context.Context, req *supbv1.GetReleaseDetailRequest) (*supbv1.GetReleaseDetailResponse, error) {
	rel, err := s.Manager.GetReleaseDetail(ctx, req.GetTag())
	if err != nil {
		return nil, mapManagerError(s.Log, err, "get release detail")
	}

	return &supbv1.GetReleaseDetailResponse{Release: releaseToProto(rel)}, nil
}

// StartUpgrade kicks off the asynchronous upgrade.
func (s *SysupgradeService) StartUpgrade(ctx context.Context, req *supbv1.StartUpgradeRequest) (*supbv1.StartUpgradeResponse, error) {
	opts := protoToOptions(req.GetOptions())

	if err := s.Manager.StartUpgrade(ctx, req.GetReleaseTag(), req.GetAssetName(), opts, req.GetForceInstallUnknownCurrent()); err != nil {
		return nil, mapManagerError(s.Log, err, "start upgrade")
	}

	return &supbv1.StartUpgradeResponse{}, nil
}

// CancelUpgrade aborts an in-flight upgrade before exec.
func (s *SysupgradeService) CancelUpgrade(ctx context.Context, _ *emptypb.Empty) (*supbv1.CancelUpgradeResponse, error) {
	if err := s.Manager.CancelUpgrade(ctx); err != nil {
		return nil, mapManagerError(s.Log, err, "cancel upgrade")
	}

	return &supbv1.CancelUpgradeResponse{}, nil
}

// GetUpgradeStatus returns the current Progress snapshot.
func (s *SysupgradeService) GetUpgradeStatus(ctx context.Context, _ *emptypb.Empty) (*supbv1.GetUpgradeStatusResponse, error) {
	p := s.Manager.GetUpgradeStatus(ctx)

	return &supbv1.GetUpgradeStatusResponse{Event: progressToProto(p)}, nil
}

// StreamUpgradeProgress streams Progress events to the client. The
// initial event is the current snapshot so callers never see an empty
// window.
func (s *SysupgradeService) StreamUpgradeProgress(ctx context.Context, _ *emptypb.Empty, stream *connect.ServerStream[supbv1.StreamUpgradeProgressResponse]) error {
	first := &supbv1.StreamUpgradeProgressResponse{Event: progressToProto(s.Manager.GetUpgradeStatus(ctx))}
	if err := stream.Send(first); err != nil {
		return fmt.Errorf("stream upgrade progress: send initial: %w", err)
	}

	ch, unsub := s.Manager.Subscribe(ctx)
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return nil
		case p, ok := <-ch:
			if !ok {
				return nil
			}

			if err := stream.Send(&supbv1.StreamUpgradeProgressResponse{Event: progressToProto(p)}); err != nil {
				return fmt.Errorf("stream upgrade progress: send: %w", err)
			}
		}
	}
}

// GetStagedImage returns the currently-staged upload, or an empty
// response when no image is staged.
func (s *SysupgradeService) GetStagedImage(_ context.Context, _ *emptypb.Empty) (*supbv1.GetStagedImageResponse, error) {
	staged := s.Manager.GetStagedImage()

	return &supbv1.GetStagedImageResponse{Image: stagedImageToProto(staged)}, nil
}

// DiscardStagedImage removes the staged image. A no-image-staged call
// is treated as success so the UI's discard button is idempotent.
func (s *SysupgradeService) DiscardStagedImage(_ context.Context, _ *emptypb.Empty) (*supbv1.DiscardStagedImageResponse, error) {
	if err := s.Manager.DiscardStagedImage(); err != nil {
		if errors.Is(err, sysupgrade.ErrNoStagedImage) {
			return &supbv1.DiscardStagedImageResponse{}, nil
		}

		return nil, mapManagerError(s.Log, err, "discard staged image")
	}

	return &supbv1.DiscardStagedImageResponse{}, nil
}

// StartLocalUpgrade kicks off the asynchronous upgrade against the
// staged image.
func (s *SysupgradeService) StartLocalUpgrade(ctx context.Context, req *supbv1.StartLocalUpgradeRequest) (*supbv1.StartLocalUpgradeResponse, error) {
	opts := protoToOptions(req.GetOptions())

	if err := s.Manager.StartLocalUpgrade(ctx, opts, req.GetSkipPreflight(), req.GetForceInstallUnknownCurrent()); err != nil {
		return nil, mapManagerError(s.Log, err, "start local upgrade")
	}

	return &supbv1.StartLocalUpgradeResponse{}, nil
}

// stagedImageToProto translates a sysupgrade.StagedImage into the
// proto representation. Returns nil when the input is nil so the
// response field is left unset.
func stagedImageToProto(s *sysupgrade.StagedImage) *supbv1.StagedImage {
	if s == nil {
		return nil
	}

	out := &supbv1.StagedImage{
		Filename:         s.Filename,
		SizeBytes:        s.SizeBytes,
		Sha256:           s.Sha256,
		PreflightOk:      s.PreflightOK,
		PreflightError:   s.PreflightError,
		MetadataPresent:  s.MetadataPresent,
		CompatVersion:    s.CompatVersion,
		CompatMessage:    s.CompatMessage,
		SupportedDevices: s.SupportedDevices,
		DeviceCompat:     s.DeviceCompat,
		ImageCompatible:  s.ImageCompatible,
	}

	if !s.UploadedAt.IsZero() {
		out.UploadedAt = timestamppb.New(s.UploadedAt)
	}

	return out
}

// mapManagerError translates a sysupgrade.* sentinel error into the
// appropriate ConnectRPC error code.
func mapManagerError(log zerolog.Logger, err error, op string) error {
	switch {
	case errors.Is(err, sysupgrade.ErrUnknownCurrentVersion),
		errors.Is(err, sysupgrade.ErrNoMatchingAsset),
		errors.Is(err, sysupgrade.ErrAmbiguousAsset),
		errors.Is(err, sysupgrade.ErrInsufficientSpace),
		errors.Is(err, sysupgrade.ErrManifestNotFound),
		errors.Is(err, sysupgrade.ErrUpgradeAlreadyExecuting),
		errors.Is(err, sysupgrade.ErrNoUpgradeInProgress),
		errors.Is(err, sysupgrade.ErrUploadInFlight),
		errors.Is(err, sysupgrade.ErrNoStagedImage),
		errors.Is(err, sysupgrade.ErrStagedPreflightFailed),
		errors.Is(err, sysupgrade.ErrBusy):
		log.Warn().Err(err).Str("op", op).Msg("sysupgrade: precondition failed")

		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, sysupgrade.ErrChecksumMismatch),
		errors.Is(err, sysupgrade.ErrChecksumNotFound):
		log.Error().Err(err).Str("op", op).Msg("sysupgrade: data integrity failure")

		return connect.NewError(connect.CodeDataLoss, err)
	case errors.Is(err, sysupgrade.ErrOptionConflict):
		log.Warn().Err(err).Str("op", op).Msg("sysupgrade: invalid options")

		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, sysupgrade.ErrReleaseNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	}

	log.Error().Err(err).Str("op", op).Msg("sysupgrade: internal error")

	return connect.NewError(connect.CodeInternal, fmt.Errorf("%s: %w", op, err))
}

// systemInfoToProto translates a sysupgrade.SystemInfo into the proto.
func systemInfoToProto(in *sysupgrade.SystemInfo) *supbv1.SystemInfo {
	if in == nil {
		return nil
	}

	return &supbv1.SystemInfo{
		Hostname:                in.Hostname,
		Distribution:            in.Distribution,
		Release:                 in.Release,
		Revision:                in.Revision,
		Target:                  in.Target,
		BoardName:               in.BoardName,
		Model:                   in.Model,
		Description:             in.Description,
		OpenmanetVersion:        in.OpenmanetVersion,
		Kernel:                  in.Kernel,
		Architecture:            in.Architecture,
		BuildDate:               in.BuildDate,
		SysupgradeCapable:       in.SysupgradeCapable,
		SysupgradeCapableReason: in.SysupgradeCapableReason,
		RootfsType:              in.RootfsType,
	}
}

// updateToProto translates a sysupgrade.Update into the proto.
func updateToProto(u sysupgrade.Update) *supbv1.Update {
	return &supbv1.Update{
		Release:          releaseToProto(u.Release),
		MatchedAsset:     assetToProto(u.MatchedAsset),
		NewerThanCurrent: u.NewerThanCurrent,
	}
}

// releaseToProto translates a sysupgrade.Release into the proto.
func releaseToProto(r sysupgrade.Release) *supbv1.Release {
	out := &supbv1.Release{
		Tag:        r.Tag,
		Name:       r.Name,
		Body:       r.Body,
		Prerelease: r.Prerelease,
		Version:    r.Version,
		Assets:     make([]*supbv1.Asset, 0, len(r.Assets)),
	}

	if !r.PublishedAt.IsZero() {
		out.PublishedAt = timestamppb.New(r.PublishedAt)
	}

	for _, a := range r.Assets {
		out.Assets = append(out.Assets, assetToProto(a))
	}

	return out
}

// assetToProto translates a sysupgrade.Asset into the proto.
func assetToProto(a sysupgrade.Asset) *supbv1.Asset {
	return &supbv1.Asset{
		Name:        a.Name,
		DownloadUrl: a.DownloadURL,
		SizeBytes:   a.SizeBytes,
	}
}

// progressToProto translates a sysupgrade.Progress into the proto.
func progressToProto(p sysupgrade.Progress) *supbv1.ProgressEvent {
	out := &supbv1.ProgressEvent{
		Phase:      phaseToProto(p.Phase),
		Percent:    p.Percent,
		BytesDone:  p.BytesDone,
		BytesTotal: p.BytesTotal,
		Message:    p.Message,
		Error:      p.ErrMsg,
		ReleaseTag: p.ReleaseTag,
		AssetName:  p.AssetName,
		ChildPid:   p.ChildPID,
	}

	if !p.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(p.UpdatedAt)
	}

	return out
}

// phaseToProto maps the package's Phase enum onto the proto enum.
func phaseToProto(p sysupgrade.Phase) supbv1.Phase {
	switch p {
	case sysupgrade.PhaseIdle:
		return supbv1.Phase_PHASE_IDLE
	case sysupgrade.PhaseChecking:
		return supbv1.Phase_PHASE_CHECKING
	case sysupgrade.PhaseDownloading:
		return supbv1.Phase_PHASE_DOWNLOADING
	case sysupgrade.PhaseVerifying:
		return supbv1.Phase_PHASE_VERIFYING
	case sysupgrade.PhaseReady:
		return supbv1.Phase_PHASE_READY
	case sysupgrade.PhaseUpgrading:
		return supbv1.Phase_PHASE_UPGRADING
	case sysupgrade.PhaseFailed:
		return supbv1.Phase_PHASE_FAILED
	}

	return supbv1.Phase_PHASE_UNSPECIFIED
}

// protoToOptions maps the proto SysupgradeOptions onto the package
// struct.
func protoToOptions(p *supbv1.SysupgradeOptions) sysupgrade.SysupgradeOptions {
	if p == nil {
		return sysupgrade.SysupgradeOptions{}
	}

	return sysupgrade.SysupgradeOptions{
		NoPreserveConfig:   p.GetNoPreserveConfig(),
		PreserveChangedEtc: p.GetPreserveChangedEtc(),
		PreserveOverlay:    p.GetPreserveOverlay(),
		SkipPackageFiles:   p.GetSkipPackageFiles(),
		IncludeEtcConfig:   p.GetIncludeEtcConfig(),
		ConfigArchivePath:  p.GetConfigArchivePath(),
		TestOnly:           p.GetTestOnly(),
		Force:              p.GetForce(),
		Quiet:              p.GetQuiet(),
		Verbose:            p.GetVerbose(),
		BackupPath:         p.GetBackupPath(),
		RestorePath:        p.GetRestorePath(),
		PreservePartitions: p.GetPreservePartitions(),
		ErasePartitions:    p.GetErasePartitions(),
	}
}
