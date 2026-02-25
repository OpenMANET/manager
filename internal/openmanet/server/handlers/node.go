package handlers

import (
	"context"

	"connectrpc.com/connect"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

type NodeService struct {
	DB  *models.Queries
	Log zerolog.Logger
}

func (n *NodeService) ListNodes(ctx context.Context, _ *emptypb.Empty) (*serviceproto.ListNodesResponse, error) {
	n.Log.Debug().Msg("ListNodes Request Received")

	nodes, err := n.DB.ListMeshNodes(ctx)
	if err != nil {
		n.Log.Error().Err(err).Msg("Failed to list mesh nodes")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protoNodes := make([]*proto.Node, 0, len(nodes))
	for _, node := range nodes {
		protoNodes = append(protoNodes, &proto.Node{
			Mac:      node.MacAddr,
			Hostname: node.Hostname,
			Ipaddr:   node.IpAddr,
			Position: &proto.Position{
				Latitude:  node.Latitude.Float64,
				Longitude: node.Longitude.Float64,
				Altitude:  float32(node.Altitude.Float64),
			},
		})
	}

	return &serviceproto.ListNodesResponse{Nodes: protoNodes}, nil
}

func (n *NodeService) GetNode(ctx context.Context, req *serviceproto.GetNodeRequest) (*serviceproto.GetNodeResponse, error) {
	n.Log.Debug().Msgf("GetNode Request Received for hostname: %s", req.Hostname)

	node, err := n.DB.GetMeshNodeByHostname(ctx, req.Hostname)
	if err != nil {
		n.Log.Error().Err(err).Msgf("Failed to get mesh node with hostname: %s", req.Hostname)

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return &serviceproto.GetNodeResponse{
		Node: &proto.Node{
			Mac:      node.MacAddr,
			Hostname: node.Hostname,
			Ipaddr:   node.IpAddr,
		},
	}, nil
}
