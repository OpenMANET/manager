package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/rs/zerolog"
)

//go:embed schema.sql
var ddl string    //nolint:gochecknoglobals
var sqlDB *sql.DB //nolint:gochecknoglobals

// NewConnection establishes a new SQLite database connection and returns a Queries instance.
// It opens a connection to the SQLite database at the specified file path with foreign keys enabled,
// verifies the connection is valid by pinging the database, executes the DDL schema to initialize
// or update the database structure, and returns a Queries instance for executing database operations.
//
// Parameters:
//   - ctx: Context for managing the database connection lifecycle and cancellation
//   - dbFilePath: File system path to the SQLite database file
//
// Returns:
//   - *models.Queries: A queries instance for database operations
//   - error: An error if the connection fails, ping fails, or DDL execution fails
func NewConnection(ctx context.Context, log zerolog.Logger, dbFilePath string) (*models.Queries, error) {
	db, err := sql.Open("sqlite3", "file:"+dbFilePath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Verify that the connection is valid
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	// Configure connection pool settings for SQLite
	// SQLite only supports 1 writer at a time, so limit max open connections
	db.SetMaxOpenConns(1)
	// Keep idle connections for reuse
	db.SetMaxIdleConns(1)
	// Don't keep connections open indefinitely
	db.SetConnMaxLifetime(0)

	if _, err := db.ExecContext(ctx, ddl); err != nil {
		log.Error().Err(err).Msg("Failed to execute DDL schema")

		return nil, fmt.Errorf("execute DDL schema: %w", err)
	}

	sqlDB = db

	queries := models.New(db)

	log.Info().Msg("Database connection established")

	return queries, nil
}

func CloseConnection() error {
	return sqlDB.Close()
}
