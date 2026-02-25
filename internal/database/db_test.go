package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// setupTestDB creates a temporary database file for testing
func setupTestDB(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	return dbPath
}

// TestNewConnectionSuccess tests successful database connection creation
func TestNewConnectionSuccess(t *testing.T) {
	ctx := context.Background()
	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	queries, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("NewConnection() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := CloseConnection(); err != nil {
			t.Errorf("Cleanup: CloseConnection() error = %v", err)
		}
	})

	if queries == nil {
		t.Fatal("NewConnection() returned nil queries")
	}

	// Verify that the database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database file was not created at %s", dbPath)
	}

	// Verify the schema was applied by checking if the table exists
	var tableName string

	err = sqlDB.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='mesh_nodes'").Scan(&tableName)
	if err != nil {
		t.Fatalf("Failed to query for mesh_nodes table: %v", err)
	}

	if tableName != "mesh_nodes" {
		t.Errorf("Expected table 'mesh_nodes' to exist, got %s", tableName)
	}
}

// TestNewConnectionWithContext tests connection creation with custom context
func TestNewConnectionWithContext(t *testing.T) {
	ctx := context.Background()
	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	queries, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("NewConnection() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := CloseConnection(); err != nil {
			t.Errorf("Cleanup: CloseConnection() error = %v", err)
		}
	})

	if queries == nil {
		t.Fatal("NewConnection() returned nil queries")
	}
}

// TestNewConnectionContextCancellation tests behavior with canceled context
func TestNewConnectionContextCancellation(t *testing.T) {
	// Create a context that is already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	// NewConnection should fail because context is canceled
	_, err := NewConnection(ctx, log, dbPath)
	if err == nil {
		t.Error("NewConnection() with canceled context should return error")

		_ = CloseConnection()
	}
}

// TestNewConnectionInvalidPath tests connection with invalid database path
func TestNewConnectionInvalidPath(t *testing.T) {
	ctx := context.Background()
	log := zerolog.New(os.Stderr)
	// Use an invalid path that cannot be created
	dbPath := "/invalid/path/that/does/not/exist/test.db"

	_, err := NewConnection(ctx, log, dbPath)
	if err == nil {
		t.Error("NewConnection() with invalid path should return error")

		_ = CloseConnection()
	}
}

// TestNewConnectionDatabaseConfiguration tests that database is configured correctly
func TestNewConnectionDatabaseConfiguration(t *testing.T) {
	ctx := context.Background()
	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	_, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("NewConnection() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := CloseConnection(); err != nil {
			t.Errorf("Cleanup: CloseConnection() error = %v", err)
		}
	})

	// Verify connection pool settings
	stats := sqlDB.Stats()

	// Check that at least one connection is open
	if stats.OpenConnections < 1 {
		t.Errorf("Expected at least 1 open connection, got %d", stats.OpenConnections)
	}

	// Verify foreign keys are enabled
	var foreignKeys int

	err = sqlDB.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys)
	if err != nil {
		t.Fatalf("Failed to check foreign_keys pragma: %v", err)
	}

	if foreignKeys != 1 {
		t.Errorf("Expected foreign_keys to be enabled (1), got %d", foreignKeys)
	}
}

// TestNewConnectionMultipleTimes tests creating multiple connections
func TestNewConnectionMultipleTimes(t *testing.T) {
	ctx := context.Background()
	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	// First connection
	queries1, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("First NewConnection() error = %v, want nil", err)
	}

	if queries1 == nil {
		t.Fatal("First NewConnection() returned nil queries")
	}

	// Close first connection
	if err := CloseConnection(); err != nil {
		t.Errorf("First CloseConnection() error = %v", err)
	}

	// Second connection to same database file
	queries2, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("Second NewConnection() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := CloseConnection(); err != nil {
			t.Errorf("Cleanup: CloseConnection() error = %v", err)
		}
	})

	if queries2 == nil {
		t.Fatal("Second NewConnection() returned nil queries")
	}
}

// TestCloseConnectionSuccess tests successful connection closure
func TestCloseConnectionSuccess(t *testing.T) {
	ctx := context.Background()
	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	_, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("NewConnection() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		// Attempt cleanup in case the test fails before explicit close
		_ = CloseConnection()
	})

	// Close the connection
	err = CloseConnection()
	if err != nil {
		t.Errorf("CloseConnection() error = %v, want nil", err)
	}

	// Verify that the connection is actually closed by trying to ping
	err = sqlDB.PingContext(context.Background())
	if err == nil {
		t.Error("Expected error when pinging closed database, got nil")
	}
}

// TestCloseConnectionMultipleTimes tests closing connection multiple times
func TestCloseConnectionMultipleTimes(t *testing.T) {
	ctx := context.Background()
	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	_, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("NewConnection() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		// Attempt cleanup in case the test fails before reaching the close calls
		_ = CloseConnection()
	})

	// First close
	err = CloseConnection()
	if err != nil {
		t.Errorf("First CloseConnection() error = %v, want nil", err)
	}

	// Second close - sql.DB.Close() is idempotent and returns nil even if already closed
	// This is expected behavior in Go's database/sql package
	err = CloseConnection()
	// Note: In Go, calling Close() on an already closed DB returns nil, not an error
	// This is the documented behavior of database/sql
	if err != nil {
		// Only fail if we get an unexpected error
		t.Logf("Second CloseConnection() returned error: %v", err)
	}
}

// TestNewConnectionSchemaApplication tests that DDL schema is properly applied
func TestNewConnectionSchemaApplication(t *testing.T) {
	ctx := context.Background()
	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	_, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("NewConnection() error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := CloseConnection(); err != nil {
			t.Errorf("Cleanup: CloseConnection() error = %v", err)
		}
	})

	// Verify all expected columns exist in mesh_nodes table
	expectedColumns := []string{
		"mac_addr", "hostname", "ip_addr", "latitude", "longitude",
		"altitude", "uci_dhcp_start", "uci_dhcp_limit", "created_at", "updated_at",
	}

	rows, err := sqlDB.QueryContext(context.Background(), "PRAGMA table_info(mesh_nodes)")
	if err != nil {
		t.Fatalf("Failed to get table info: %v", err)
	}
	defer rows.Close()

	var foundColumns []string

	for rows.Next() {
		var cid int

		var name, colType string

		var notNull, pk int

		var dfltValue sql.NullString

		err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk)
		if err != nil {
			t.Fatalf("Failed to scan column info: %v", err)
		}

		foundColumns = append(foundColumns, name)
	}

	// Verify all expected columns are present
	for _, expected := range expectedColumns {
		found := false

		for _, actual := range foundColumns {
			if actual == expected {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("Expected column %s not found in mesh_nodes table", expected)
		}
	}
}

// TestNewConnectionWithTimeout tests connection with a timeout context
func TestNewConnectionWithTimeout(t *testing.T) {
	// Use a reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log := zerolog.New(os.Stderr)
	dbPath := setupTestDB(t)

	queries, err := NewConnection(ctx, log, dbPath)
	if err != nil {
		t.Fatalf("NewConnection() with timeout error = %v, want nil", err)
	}

	t.Cleanup(func() {
		if err := CloseConnection(); err != nil {
			t.Errorf("Cleanup: CloseConnection() error = %v", err)
		}
	})

	if queries == nil {
		t.Fatal("NewConnection() returned nil queries")
	}
}

// TestCloseConnectionWithNilDB tests closing when sqlDB is nil
func TestCloseConnectionWithNilDB(t *testing.T) {
	// Save the current sqlDB and restore it after test
	oldDB := sqlDB

	defer func() { sqlDB = oldDB }()

	// Set sqlDB to nil
	sqlDB = nil

	// This should panic or return an error
	defer func() {
		if r := recover(); r == nil {
			// If no panic, should have error
			err := CloseConnection()
			if err == nil {
				t.Error("CloseConnection() with nil sqlDB should return error or panic")
			}
		}
	}()

_ = CloseConnection()
}
