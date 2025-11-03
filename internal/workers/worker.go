package worker

import (
	"context"
	"time"
)

type Worker struct {
	Stopped      bool
	ShutdownChan <-chan any
	Interval     time.Duration // Interval between work cycles
}

// NewWorker creates and returns a new Worker instance.
// It initializes the Worker with the provided shutdown channel and interval.
// The Worker will use the shutdownChan to listen for shutdown signals and
// interval to determine its operation frequency.
//
// Parameters:
//   - shutdownChan: a receive-only channel used to signal shutdown.
//   - interval: the duration between worker operations.
//
// Returns:
//   - A pointer to the newly created Worker.
func NewWorker(shutdownChan <-chan any, interval time.Duration) *Worker {
	return &Worker{
		Stopped:      false,
		ShutdownChan: shutdownChan,
		Interval:     interval,
	}
}

// ShouldStop returns true if the worker has been stopped, otherwise false.
func (w *Worker) ShouldStop() bool {
	return w.Stopped
}

// Stop sets the Stopped flag to true, indicating that the worker should cease its operations.
func (w *Worker) Stop() {
	w.Stopped = true
}

// Run starts the worker loop, periodically performing work at intervals specified by w.Interval.
// The loop listens for shutdown signals on w.ShutdownChan and stops the worker gracefully when received.
// If ShouldStop returns true, the loop exits and the worker stops.
// This method blocks until the worker is stopped.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ShutdownChan:
			w.Stop()
		case <-ticker.C:
			if w.ShouldStop() {
				return
			}
			// Perform work here
		}
	}
}
