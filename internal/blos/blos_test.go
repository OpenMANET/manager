package blos

import (
	"context"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"tailscale.com/types/key"
)

// TestBLOSWithStatusWorker tests that BLOS properly initializes and manages the status worker.
func TestBLOSWithStatusWorker(t *testing.T) {
	// Create a mock config
	v := viper.New()
	v.Set("BLOS.statusWorkerInterval", 1) // 1 second for testing
	v.Set("alfred.batInterface", "bat0")
	cfg := config.New(v)

	logger := zerolog.Nop()

	// Create mock status client
	mockClient := &MockStatusClient{}
	mockStatus := createMockStatus()
	mockClient.SetStatus(mockStatus)

	// Create BLOS with a custom status worker for testing
	r := &BLOS{
		Config: cfg,
		Logger: logger,
		ctx:    context.Background(),
	}

	// Initialize the status worker with our mock client
	interval := time.Duration(cfg.GetBLOSStatusWorkerInterval()) * time.Second
	r.statusWorker = NewStatusWorker(mockClient, interval, logger)
	r.statusWorker.Start()

	// Give it time to fetch status
	time.Sleep(100 * time.Millisecond)

	// Test GetPeers
	peers := r.GetPeers()
	if peers == nil {
		t.Fatal("Expected peers to be non-nil")
	}

	if len(peers) != len(mockStatus.Peer) {
		t.Errorf("Expected %d peers, got %d", len(mockStatus.Peer), len(peers))
	}

	// Test GetStatus
	status := r.GetStatus()
	if status == nil {
		t.Fatal("Expected status to be non-nil")
	}

	if status.BackendState != "Running" {
		t.Errorf("Expected BackendState 'Running', got '%s'", status.BackendState)
	}

	// Test GetPeer
	var testKey key.NodePublic
	for k := range mockStatus.Peer {
		testKey = k
		break
	}

	peer, ok := r.GetPeer(testKey)
	if !ok {
		t.Error("Expected to find peer")
	}
	if peer == nil {
		t.Error("Expected peer to be non-nil")
	}

	// Test Stop
	r.Stop()
	time.Sleep(50 * time.Millisecond)

	if r.statusWorker.IsRunning() {
		t.Error("Expected status worker to be stopped")
	}
}

// TestBLOSGetPeersWhenWorkerIsNil tests that BLOS handles nil worker gracefully.
func TestBLOSGetPeersWhenWorkerIsNil(t *testing.T) {
	v := viper.New()
	cfg := config.New(v)
	logger := zerolog.Nop()

	r := &BLOS{
		Config:       cfg,
		Logger:       logger,
		ctx:          context.Background(),
		statusWorker: nil, // Explicitly nil
	}

	peers := r.GetPeers()
	if peers != nil {
		t.Error("Expected GetPeers to return nil when worker is nil")
	}

	status := r.GetStatus()
	if status != nil {
		t.Error("Expected GetStatus to return nil when worker is nil")
	}

	var testKey key.NodePublic
	_, ok := r.GetPeer(testKey)
	if ok {
		t.Error("Expected GetPeer to return false when worker is nil")
	}

	// Stop should not panic
	r.Stop()
}

// TestBLOSStatusWorkerInterval tests that the interval is correctly configured.
func TestBLOSStatusWorkerInterval(t *testing.T) {
	tests := []struct {
		name             string
		configInterval   int
		expectedInterval time.Duration
	}{
		{
			name:             "default interval",
			configInterval:   0,
			expectedInterval: 30 * time.Second,
		},
		{
			name:             "custom interval",
			configInterval:   60,
			expectedInterval: 60 * time.Second,
		},
		{
			name:             "short interval",
			configInterval:   5,
			expectedInterval: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.configInterval > 0 {
				v.Set("BLOS.statusWorkerInterval", tt.configInterval)
			}
			cfg := config.New(v)
			logger := zerolog.Nop()

			mockClient := &MockStatusClient{}
			interval := time.Duration(cfg.GetBLOSStatusWorkerInterval()) * time.Second

			if interval != tt.expectedInterval {
				t.Errorf("Expected interval %v, got %v", tt.expectedInterval, interval)
			}

			worker := NewStatusWorker(mockClient, interval, logger)
			if worker.interval != tt.expectedInterval {
				t.Errorf("Expected worker interval %v, got %v", tt.expectedInterval, worker.interval)
			}
		})
	}
}

// TestBLOSConcurrentAccess tests concurrent access to BLOS peer data.
func TestBLOSConcurrentAccess(t *testing.T) {
	v := viper.New()
	v.Set("BLOS.statusWorkerInterval", 1)
	cfg := config.New(v)
	logger := zerolog.Nop()

	mockClient := &MockStatusClient{}
	mockClient.SetStatus(createMockStatus())

	r := &BLOS{
		Config: cfg,
		Logger: logger,
		ctx:    context.Background(),
	}

	interval := time.Duration(cfg.GetBLOSStatusWorkerInterval()) * time.Second
	r.statusWorker = NewStatusWorker(mockClient, interval, logger)
	r.statusWorker.Start()
	defer r.Stop()

	// Wait for initial fetch
	time.Sleep(100 * time.Millisecond)

	// Spawn multiple goroutines accessing peer data
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = r.GetPeers()
				_ = r.GetStatus()
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we get here without panic, the test passes
}
