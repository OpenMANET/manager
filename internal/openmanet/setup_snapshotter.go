package openmanet

import (
	"context"

	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
)

// wizardSnapshotter adapts the concrete network.FileSystemUCISnapshotter
// to the handlers.UCISnapshotter interface used by the SetupService.
//
// The concrete type returns *network.FileSystemUCISnapshot from
// Snapshot, but the handlers package's interface wants
// handlers.UCISnapshot. Defining the adapter here (rather than in
// the network package) keeps the network package free of any
// dependency on handlers, avoiding a cycle.
type wizardSnapshotter struct {
	inner *network.FileSystemUCISnapshotter
}

// newWizardSnapshotter constructs the production snapshotter wired
// to the supplied wireless reader's underlying UCI tree. The reader
// must be the same instance the SetupService receives as `UCI` so
// the post-restore tree reload syncs the wizard's in-memory view
// with the restored on-disk state.
func newWizardSnapshotter(reader *network.UCIWirelessConfigReader) handlers.UCISnapshotter {
	return &wizardSnapshotter{
		inner: &network.FileSystemUCISnapshotter{
			TreePath: "/etc/config",
			Tree:     reader.Tree(),
		},
	}
}

// Snapshot delegates to the inner snapshotter and returns the
// concrete snapshot pointer as the handlers.UCISnapshot interface.
func (s *wizardSnapshotter) Snapshot(ctx context.Context, configs []string) (handlers.UCISnapshot, error) {
	snap, err := s.inner.Snapshot(ctx, configs)
	if err != nil {
		return nil, err
	}

	return snap, nil
}

// Restore type-asserts the supplied opaque snapshot back to the
// concrete type and delegates to the inner snapshotter. A snapshot
// produced by a different snapshotter implementation is ignored
// (returns nil) so a mis-wired SetupService can't crash production.
func (s *wizardSnapshotter) Restore(ctx context.Context, snapshot handlers.UCISnapshot) error {
	concrete, ok := snapshot.(*network.FileSystemUCISnapshot)
	if !ok {
		return nil
	}

	return s.inner.Restore(ctx, concrete)
}
