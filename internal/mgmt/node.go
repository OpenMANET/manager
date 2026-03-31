package mgmt

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/network"
)

// alfredClient abstracts the alfred.Client methods used by management workers,
// allowing tests to inject a fake implementation.
type alfredClient interface {
	Set(dataType uint8, version uint8, payload []byte) error
	Request(dataType uint8) ([]alfred.Record, error)
}

const (
	NodeDataType        uint8 = uint8(proto.DataType_DATA_TYPE_NODE)
	NodeDataTypeVersion uint8 = 1
)

type NodeDataWorker struct {
	Config   *ManagementConfig
	Client   *alfred.Client
	done     <-chan struct{}
	Interval time.Duration
}

func NewNodeDataWorker(config *ManagementConfig, client *alfred.Client, interval time.Duration, ctx context.Context) *NodeDataWorker {
	config.Log.Info().Msg("NodeDataWorker initialized")

	return &NodeDataWorker{
		Config:   config,
		Client:   client,
		Interval: interval,
		done:     ctx.Done(),
	}
}

// Start begins the periodic sending of node data to the Alfred client.
func (ndw *NodeDataWorker) StartSend() { //nolint:gocognit
	ticker := time.NewTicker(ndw.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ndw.done:
			return
		case <-ticker.C:
			if err := ndw.sendNodeDataOnce(ndw.Client); err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error in node data send tick")
			}
		}
	}
}

// sendNodeDataOnce performs a single iteration of the node data send logic.
// It checks DHCP configuration, gathers interface/hostname/GPS data, and
// publishes the result via the provided alfred client.
func (ndw *NodeDataWorker) sendNodeDataOnce(client alfredClient) error {
	return ndw.sendNodeDataOnceWithDeps(client, ndw.Config.uciOpenMANETConfig, ndw.Config.uciDHCPConfig,
		network.GetInterfaceByName, os.Hostname)
}

// sendNodeDataOnceWithDeps is the dependency-injectable version of sendNodeDataOnce.
func (ndw *NodeDataWorker) sendNodeDataOnceWithDeps(
	client alfredClient,
	openMANETReader network.OpenMANETConfigReader,
	dhcpReader network.DHCPConfigReader,
	getIface func(string) network.NetworkInterface,
	getHostname func() (string, error),
) error {
	configured, err := network.IsDHCPConfiguredWithReader(openMANETReader)
	if err != nil {
		return fmt.Errorf("check DHCP configuration: %w", err)
	}

	if !configured {
		ndw.Config.Log.Debug().Msg("Static Address & DHCP not configured, skipping node data send")

		return nil
	}

	// Get interface information
	iface := getIface(ndw.Config.IFace)

	hostname, err := getHostname()
	if err != nil {
		ndw.Config.Log.Error().Err(err).Msg("Error getting hostname")

		hostname = "unknown"
	}

	// if ndw.Config.IFace is prefixed with "br-", remove the prefix because dhcp and network config is tied to the physical interface
	normalizedIface := ndw.Config.IFace
	if after, ok := strings.CutPrefix(ndw.Config.IFace, "br-"); ok {
		normalizedIface = after
	}

	// Get DHCP info from UCI
	dhcp, err := network.GetDHCPConfigWithReader(normalizedIface, dhcpReader)
	if err != nil {
		return fmt.Errorf("get DHCP configuration: %w", err)
	}

	if len(iface.IP) == 0 {
		return fmt.Errorf("interface %s has no IP address", ndw.Config.IFace)
	}

	nodeData := proto.Node{
		Mac:          iface.MAC,
		Hostname:     hostname,
		Ipaddr:       iface.IP[0].IP.String(),
		UciDhcpStart: dhcp.Start,
		UciDhcpLimit: dhcp.Limit,
	}

	// Get position data if GPS is available
	if ndw.Config.GPS != nil {
		positionReport := ndw.Config.GPS.GetPosition()
		if positionReport.Mode <= 1 {
			ndw.Config.Log.Debug().Msg("No valid GPS position available")
		}

		if positionReport.Mode > 1 {
			ndw.Config.Log.Debug().
				Float64("lat", positionReport.Latitude).
				Float64("lon", positionReport.Longitude).
				Float64("alt", positionReport.Altitude).
				Uint8("mode", uint8(positionReport.Mode)).
				Msg("Current GPS position")

			nodeData.Position = &proto.Position{
				Latitude:  positionReport.Latitude,
				Longitude: positionReport.Longitude,
				Altitude:  float32(positionReport.Altitude),
			}
		}
	} else {
		ndw.Config.Log.Debug().Msg("GPS service not available")
	}

	nodeDataBytes, err := nodeData.MarshalVT()
	if err != nil {
		return fmt.Errorf("marshal node data: %w", err)
	}

	if err := client.Set(NodeDataType, NodeDataTypeVersion, nodeDataBytes); err != nil {
		return fmt.Errorf("send node data: %w", err)
	}

	return nil
}

func (ndw *NodeDataWorker) StartReceive() { //nolint:gocognit
	ticker := time.NewTicker(ndw.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ndw.done:
			return
		case <-ticker.C:
			if err := ndw.receiveNodeDataOnce(ndw.Client, os.Hostname); err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error in node data receive tick")
			}
		}
	}
}

// receiveNodeDataOnce performs a single iteration of the node data receive logic.
// It requests node data records from the alfred client, filters out records for the
// local hostname, and persists the rest to the database.
func (ndw *NodeDataWorker) receiveNodeDataOnce(client alfredClient, getHostname func() (string, error)) error {
	records, err := client.Request(NodeDataType)
	if err != nil {
		return fmt.Errorf("request node data: %w", err)
	}

	hostname, err := getHostname()
	if err != nil {
		ndw.Config.Log.Error().Err(err).Msg("Error getting hostname")
	}

	for _, rec := range records {
		var nodeData proto.Node

		if err := nodeData.UnmarshalVT(rec.Data); err != nil {
			ndw.Config.Log.Error().Err(err).Msg("Error unmarshaling node data")

			continue
		}

		// ignore our own node data
		if nodeData.Hostname == hostname {
			continue
		}

		ndw.Config.Log.Debug().Msgf("Received node data: %+v", &nodeData)

		if err := ndw.RecordNodeData(&nodeData); err != nil {
			ndw.Config.Log.Error().Err(err).Msg("Error recording node data")
		}
	}

	return nil
}

// RecordNodeData persists node information to the database by creating or updating
// a mesh node record. It converts the protobuf Node data into database model parameters,
// handling optional DHCP configuration fields (UciDhcpStart and UciDhcpLimit) by parsing
// them into nullable int64 values. Any parsing errors are logged and returned immediately.
// If database insertion fails, the error is logged but not returned, allowing the function
// to complete successfully despite database errors.
//
// Parameters:
//   - nodeData: A protobuf Node message containing mesh node information including
//     MAC address, IP address, hostname, position, and optional DHCP settings
//
// Returns:
//   - error: Returns an error if DHCP field parsing fails, otherwise returns nil
//     even if database insertion fails
func (ndw *NodeDataWorker) RecordNodeData(nodeData *proto.Node) error { //nolint:gocognit
	var dhcpStart, dhcpLimit sql.NullInt64

	ctx := context.Background()

	if nodeData.UciDhcpStart != "" {
		start, err := strconv.ParseInt(nodeData.UciDhcpStart, 10, 64)
		if err != nil {
			ndw.Config.Log.Error().Err(err).Msg("Error parsing UciDhcpStart")

			return fmt.Errorf("parse UciDhcpStart: %w", err)
		}

		dhcpStart = sql.NullInt64{Int64: start, Valid: true}
	}

	if nodeData.UciDhcpLimit != "" {
		limit, err := strconv.ParseInt(nodeData.UciDhcpLimit, 10, 64)
		if err != nil {
			ndw.Config.Log.Error().Err(err).Msg("Error parsing UciDhcpLimit")

			return fmt.Errorf("parse UciDhcpLimit: %w", err)
		}

		dhcpLimit = sql.NullInt64{Int64: limit, Valid: true}
	}

	// Insert or update node data in the database
	_, err := ndw.Config.DB.CreateMeshNode(ctx, models.CreateMeshNodeParams{
		MacAddr:      nodeData.Mac,
		IpAddr:       nodeData.Ipaddr,
		Hostname:     nodeData.Hostname,
		Latitude:     sql.NullFloat64{Float64: nodeData.Position.GetLatitude(), Valid: nodeData.Position != nil},
		Longitude:    sql.NullFloat64{Float64: nodeData.Position.GetLongitude(), Valid: nodeData.Position != nil},
		Altitude:     sql.NullFloat64{Float64: float64(nodeData.Position.GetAltitude()), Valid: nodeData.Position != nil},
		UciDhcpStart: dhcpStart,
		UciDhcpLimit: dhcpLimit,
	})
	if err != nil {
		ndw.Config.Log.Error().Err(err).Msg("Error inserting node data into database")
	}

	// Delete duplicate entries if any (should not happen due to unique constraint)
	err = ndw.Config.DB.DeleteDuplicateMeshNodes(ctx)
	if err != nil {
		ndw.Config.Log.Error().Err(err).Msg("Error deleting duplicate mesh nodes")
	}

	return nil
}
