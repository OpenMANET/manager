package handlers

import (
	"context"

	setupv1 "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1"
)

// ApplySetupStream is the test-exported alias of the package-private
// applySetupStream interface used by the SetupService phase helpers.
// External test packages can implement Send to drive ApplySetup
// without standing up a connect.ServerStream.
type ApplySetupStream interface {
	Send(*setupv1.ApplySetupResponse) error
}

// ApplySetupForTest is the testable entry point that bypasses the
// connect.Request / connect.ServerStream wrapping and lets unit tests
// exercise the full ApplySetup pipeline through a fake stream.
func (s *SetupService) ApplySetupForTest(
	ctx context.Context,
	profile *setupv1.MeshNodeProfile,
	stream ApplySetupStream,
) error {
	return s.applySetup(ctx, profile, stream)
}
