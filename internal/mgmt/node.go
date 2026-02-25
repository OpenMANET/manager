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

const (
	NodeDataType              uint8 = uint8(proto.DataType_DATA_TYPE_NODE)
	NodeDataTypeVersion       uint8 = 2
	legacyNodeDataTypeVersion uint8 = 1
)

type NodeDataWorker struct {
	Config       *ManagementConfig
	Client       *alfred.Client
	ShutdownChan <-chan os.Signal
	Interval     time.Duration
}

func NewNodeDataWorker(config *ManagementConfig, client *alfred.Client, interval time.Duration, shutdownChan <-chan os.Signal) *NodeDataWorker {
	config.Log.Info().Msg("NodeDataWorker initialized")

	return &NodeDataWorker{
		Config:       config,
		Client:       client,
		Interval:     interval,
		ShutdownChan: shutdownChan,
	}
}

// Start begins the periodic sending of node data to the Alfred client.
func (ndw *NodeDataWorker) StartSend() {
	ticker := time.NewTicker(ndw.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ndw.ShutdownChan:
			return
		case <-ticker.C:
			configured, err := network.IsDHCPConfiguredWithReader(ndw.Config.uciOpenMANETConfig)
			if err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error checking DHCP configuration")

				continue
			}

			if !configured {
				ndw.Config.Log.Debug().Msg("Static Address & DHCP not configured, skipping node data send")

				continue
			}

			// Get interface information
			iface := network.GetInterfaceByName(ndw.Config.IFace)

			hostname, err := os.Hostname()
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
			dhcp, err := network.GetDHCPConfigWithReader(normalizedIface, ndw.Config.uciDHCPConfig)
			if err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error getting DHCP configuration")

				continue
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

			var nodeDataBytes []byte

			nodeDataBytes, err = nodeData.MarshalVT()
			if err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error marshaling node data")

				continue
			}

			encryptedPayload, err := ndw.Config.payloadCodec.Encrypt(NodeDataType, nodeDataBytes)
			if err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error encrypting node data")

				continue
			}

			err = ndw.Client.Set(NodeDataType, NodeDataTypeVersion, encryptedPayload)
			if err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error sending node data")
			}
		}
	}
}

// Start begins the periodic receiving of node data from the Alfred client.
func (ndw *NodeDataWorker) StartReceive() {
	ticker := time.NewTicker(ndw.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ndw.ShutdownChan:
			return
		case <-ticker.C:
			record, err := ndw.Client.Request(NodeDataType)
			if err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error receiving node data")

				continue
			}

			for _, rec := range record {
				decodedPayload := rec.Data

				switch rec.Version {
				case NodeDataTypeVersion:
					decodedPayload, err = ndw.Config.payloadCodec.Decrypt(NodeDataType, rec.Source, rec.Data)
					if err != nil {
						ndw.Config.Log.Warn().
							Err(err).
							Str("source", rec.Source.String()).
							Msg("Dropping node data payload that failed authentication/decryption")

						continue
					}
				case legacyNodeDataTypeVersion:
					ndw.Config.Log.Debug().
						Str("source", rec.Source.String()).
						Msg("Received legacy plaintext node payload")
				default:
					ndw.Config.Log.Warn().
						Uint8("version", rec.Version).
						Str("source", rec.Source.String()).
						Msg("Dropping node data payload with unsupported version")

					continue
				}

				var nodeData proto.Node

				err = nodeData.UnmarshalVT(decodedPayload)
				if err != nil {
					ndw.Config.Log.Error().Err(err).Msg("Error unmarshaling node data")

					continue
				}

				hostname, err := os.Hostname()
				if err != nil {
					ndw.Config.Log.Error().Err(err).Msg("Error getting hostname")
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
		}
	}
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
func (ndw *NodeDataWorker) RecordNodeData(nodeData *proto.Node) error {
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
