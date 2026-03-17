package mgmt

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nodeTestSchemaSQL = `
CREATE TABLE IF NOT EXISTS mesh_nodes (
  mac_addr       text PRIMARY KEY NOT NULL,
  hostname       text NOT NULL,
  ip_addr        text NOT NULL,
  latitude       real,
  longitude      real,
  altitude       real,
  uci_dhcp_start integer,
  uci_dhcp_limit integer,
  created_at     timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

func newNodeTestDB(t *testing.T) *models.Queries {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	if _, err = db.Exec(nodeTestSchemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	return models.New(db)
}

func TestRecordNodeData(t *testing.T) {
	tests := []struct {
		name    string
		node    *proto.Node
		wantErr string
		checkDB func(t *testing.T, q *models.Queries)
	}{
		{
			name: "valid node with position",
			node: &proto.Node{
				Mac:          "aa:bb:cc:dd:ee:ff",
				Hostname:     "node-1",
				Ipaddr:       "10.0.0.1",
				UciDhcpStart: "100",
				UciDhcpLimit: "150",
				Position: &proto.Position{
					Latitude:  37.7749,
					Longitude: -122.4194,
					Altitude:  50.5,
				},
			},
			checkDB: func(t *testing.T, q *models.Queries) {
				t.Helper()

				node, err := q.GetMeshNode(context.Background(), "aa:bb:cc:dd:ee:ff")
				require.NoError(t, err)
				assert.Equal(t, "node-1", node.Hostname)
				assert.Equal(t, "10.0.0.1", node.IpAddr)
				assert.Equal(t, sql.NullFloat64{Float64: 37.7749, Valid: true}, node.Latitude)
				assert.Equal(t, sql.NullFloat64{Float64: -122.4194, Valid: true}, node.Longitude)
				assert.InDelta(t, 50.5, node.Altitude.Float64, 0.01)
				assert.True(t, node.Altitude.Valid)
				assert.Equal(t, sql.NullInt64{Int64: 100, Valid: true}, node.UciDhcpStart)
				assert.Equal(t, sql.NullInt64{Int64: 150, Valid: true}, node.UciDhcpLimit)
			},
		},
		{
			name: "valid node without position",
			node: &proto.Node{
				Mac:          "11:22:33:44:55:66",
				Hostname:     "node-2",
				Ipaddr:       "10.0.0.2",
				UciDhcpStart: "10",
				UciDhcpLimit: "20",
			},
			checkDB: func(t *testing.T, q *models.Queries) {
				t.Helper()

				node, err := q.GetMeshNode(context.Background(), "11:22:33:44:55:66")
				require.NoError(t, err)
				assert.False(t, node.Latitude.Valid)
				assert.False(t, node.Longitude.Valid)
				assert.False(t, node.Altitude.Valid)
			},
		},
		{
			name: "empty DHCP fields",
			node: &proto.Node{
				Mac:      "de:ad:be:ef:00:01",
				Hostname: "node-3",
				Ipaddr:   "10.0.0.3",
			},
			checkDB: func(t *testing.T, q *models.Queries) {
				t.Helper()

				node, err := q.GetMeshNode(context.Background(), "de:ad:be:ef:00:01")
				require.NoError(t, err)
				assert.False(t, node.UciDhcpStart.Valid)
				assert.False(t, node.UciDhcpLimit.Valid)
			},
		},
		{
			name: "invalid UciDhcpStart",
			node: &proto.Node{
				Mac:          "ff:ff:ff:ff:ff:01",
				Hostname:     "bad-start",
				Ipaddr:       "10.0.0.4",
				UciDhcpStart: "abc",
			},
			wantErr: "parse UciDhcpStart",
		},
		{
			name: "invalid UciDhcpLimit",
			node: &proto.Node{
				Mac:          "ff:ff:ff:ff:ff:02",
				Hostname:     "bad-limit",
				Ipaddr:       "10.0.0.5",
				UciDhcpStart: "10",
				UciDhcpLimit: "xyz",
			},
			wantErr: "parse UciDhcpLimit",
		},
		{
			name: "upsert overwrites existing node",
			node: &proto.Node{
				Mac:      "aa:bb:cc:dd:ee:ff",
				Hostname: "node-1-updated",
				Ipaddr:   "10.0.0.99",
			},
			checkDB: func(t *testing.T, q *models.Queries) {
				t.Helper()

				node, err := q.GetMeshNode(context.Background(), "aa:bb:cc:dd:ee:ff")
				require.NoError(t, err)
				assert.Equal(t, "node-1-updated", node.Hostname)
				assert.Equal(t, "10.0.0.99", node.IpAddr)
			},
		},
	}

	// Use a shared DB so the upsert test can build on the first insert.
	db := newNodeTestDB(t)
	cfg := newTestManagementConfig()
	cfg.DB = db

	ndw := &NodeDataWorker{
		Config:   cfg,
		Interval: time.Second,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ndw.RecordNodeData(tt.node)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)

			if tt.checkDB != nil {
				tt.checkDB(t, db)
			}
		})
	}
}
