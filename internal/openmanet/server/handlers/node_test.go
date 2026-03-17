package handlers_test

import (
	"context"
	"database/sql"
	"testing"

	"connectrpc.com/connect"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func newNodeService(t *testing.T) (*handlers.NodeService, *models.Queries) {
	t.Helper()
	db := newTestDB(t)

	return &handlers.NodeService{
		DB:  db,
		Log: zerolog.Nop(),
	}, db
}

func TestListNodes_Empty(t *testing.T) {
	svc, _ := newNodeService(t)

	resp, err := svc.ListNodes(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetNodes())
}

func TestListNodes_WithData(t *testing.T) {
	svc, db := newNodeService(t)
	ctx := context.Background()

	_, err := db.CreateMeshNode(ctx, models.CreateMeshNodeParams{
		MacAddr:   "aa:bb:cc:dd:ee:ff",
		Hostname:  "node-1",
		IpAddr:    "10.0.0.1",
		Latitude:  sql.NullFloat64{Float64: 37.7749, Valid: true},
		Longitude: sql.NullFloat64{Float64: -122.4194, Valid: true},
	})
	require.NoError(t, err)

	resp, err := svc.ListNodes(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNodes(), 1)
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", resp.GetNodes()[0].GetMac())
	assert.Equal(t, "node-1", resp.GetNodes()[0].GetHostname())
	assert.Equal(t, "10.0.0.1", resp.GetNodes()[0].GetIpaddr())
}

func TestListNodes_MultipleNodes(t *testing.T) {
	svc, db := newNodeService(t)
	ctx := context.Background()

	nodes := []models.CreateMeshNodeParams{
		{MacAddr: "11:22:33:44:55:66", Hostname: "alpha", IpAddr: "10.0.0.1"},
		{MacAddr: "aa:bb:cc:dd:ee:ff", Hostname: "beta", IpAddr: "10.0.0.2"},
		{MacAddr: "00:11:22:33:44:55", Hostname: "gamma", IpAddr: "10.0.0.3"},
	}
	for _, n := range nodes {
		_, err := db.CreateMeshNode(ctx, n)
		require.NoError(t, err)
	}

	resp, err := svc.ListNodes(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	assert.Len(t, resp.GetNodes(), 3)
}

func TestGetNode_Found(t *testing.T) {
	svc, db := newNodeService(t)
	ctx := context.Background()

	_, err := db.CreateMeshNode(ctx, models.CreateMeshNodeParams{
		MacAddr:  "de:ad:be:ef:00:01",
		Hostname: "target-node",
		IpAddr:   "192.168.1.1",
	})
	require.NoError(t, err)

	resp, err := svc.GetNode(ctx, &serviceproto.GetNodeRequest{Hostname: "target-node"})
	require.NoError(t, err)
	require.NotNil(t, resp.GetNode())
	assert.Equal(t, "target-node", resp.GetNode().GetHostname())
	assert.Equal(t, "de:ad:be:ef:00:01", resp.GetNode().GetMac())
}

func TestGetNode_NotFound(t *testing.T) {
	svc, _ := newNodeService(t)

	_, err := svc.GetNode(context.Background(), &serviceproto.GetNodeRequest{Hostname: "ghost"})
	require.Error(t, err)
}

func TestListNodes_PositionFields(t *testing.T) {
	svc, db := newNodeService(t)
	ctx := context.Background()

	_, err := db.CreateMeshNode(ctx, models.CreateMeshNodeParams{
		MacAddr:   "aa:bb:cc:dd:ee:01",
		Hostname:  "positioned-node",
		IpAddr:    "10.0.0.10",
		Latitude:  sql.NullFloat64{Float64: 51.5074, Valid: true},
		Longitude: sql.NullFloat64{Float64: -0.1278, Valid: true},
		Altitude:  sql.NullFloat64{Float64: 100.5, Valid: true},
	})
	require.NoError(t, err)

	resp, err := svc.ListNodes(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNodes(), 1)

	pos := resp.GetNodes()[0].GetPosition()
	require.NotNil(t, pos)
	assert.InDelta(t, 51.5074, pos.GetLatitude(), 0.0001)
	assert.InDelta(t, -0.1278, pos.GetLongitude(), 0.0001)
	assert.InDelta(t, 100.5, float64(pos.GetAltitude()), 0.1)
}

func TestListNodes_NullPosition(t *testing.T) {
	svc, db := newNodeService(t)
	ctx := context.Background()

	// Insert node with no position data (all NullFloat64 invalid).
	_, err := db.CreateMeshNode(ctx, models.CreateMeshNodeParams{
		MacAddr:  "aa:bb:cc:dd:ee:02",
		Hostname: "no-position-node",
		IpAddr:   "10.0.0.11",
	})
	require.NoError(t, err)

	resp, err := svc.ListNodes(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNodes(), 1)

	// Current implementation always creates a Position{} struct even for null
	// DB values — position fields will be zero-valued.
	pos := resp.GetNodes()[0].GetPosition()
	require.NotNil(t, pos, "Position struct is always created by ListNodes")
	assert.Equal(t, float64(0), pos.GetLatitude())
	assert.Equal(t, float64(0), pos.GetLongitude())
	assert.Equal(t, float32(0), pos.GetAltitude())
}

func TestGetNode_ConnectErrorCode(t *testing.T) {
	svc, _ := newNodeService(t)

	_, err := svc.GetNode(context.Background(), &serviceproto.GetNodeRequest{Hostname: "nonexistent"})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
}

func TestGetNode_PositionNotReturned(t *testing.T) {
	svc, db := newNodeService(t)
	ctx := context.Background()

	_, err := db.CreateMeshNode(ctx, models.CreateMeshNodeParams{
		MacAddr:   "aa:bb:cc:dd:ee:03",
		Hostname:  "has-position",
		IpAddr:    "10.0.0.12",
		Latitude:  sql.NullFloat64{Float64: 40.7128, Valid: true},
		Longitude: sql.NullFloat64{Float64: -74.0060, Valid: true},
		Altitude:  sql.NullFloat64{Float64: 10.0, Valid: true},
	})
	require.NoError(t, err)

	resp, err := svc.GetNode(ctx, &serviceproto.GetNodeRequest{Hostname: "has-position"})
	require.NoError(t, err)
	require.NotNil(t, resp.GetNode())

	// GetNode currently does not populate Position in the response proto.
	assert.Nil(t, resp.GetNode().GetPosition(), "GetNode omits Position field")
}

func TestListNodes_OrderByHostname(t *testing.T) {
	svc, db := newNodeService(t)
	ctx := context.Background()

	for _, n := range []models.CreateMeshNodeParams{
		{MacAddr: "00:00:00:00:00:03", Hostname: "charlie", IpAddr: "10.0.0.3"},
		{MacAddr: "00:00:00:00:00:01", Hostname: "alpha", IpAddr: "10.0.0.1"},
		{MacAddr: "00:00:00:00:00:02", Hostname: "bravo", IpAddr: "10.0.0.2"},
	} {
		_, err := db.CreateMeshNode(ctx, n)
		require.NoError(t, err)
	}

	resp, err := svc.ListNodes(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNodes(), 3)

	assert.Equal(t, "alpha", resp.GetNodes()[0].GetHostname())
	assert.Equal(t, "bravo", resp.GetNodes()[1].GetHostname())
	assert.Equal(t, "charlie", resp.GetNodes()[2].GetHostname())
}
