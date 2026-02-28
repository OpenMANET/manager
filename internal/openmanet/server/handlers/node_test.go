package handlers_test

import (
	"context"
	"database/sql"
	"testing"

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
