package mgmt

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	NodeDataType        uint8 = uint8(proto.DataType_DATA_TYPE_NODE)
	NodeDataTypeVersion uint8 = 1
)

type NodeDataWorker struct {
	Config       *ManagementConfig
	Client       *alfred.Client
	Interval     time.Duration
	ShutdownChan <-chan os.Signal
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

			nodeData := proto.Node{
				Mac:          iface.MAC,
				Hostname:     hostname,
				Ipaddr:       iface.IP[0].IP.String(),
				UciDhcpStart: dhcp.Start,
				UciDhcpLimit: dhcp.Limit,
			}

			var nodeDataBytes []byte
			nodeDataBytes, err = nodeData.MarshalVT()
			if err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error marshaling node data")
				continue
			}

			err = ndw.Client.Set(NodeDataType, NodeDataTypeVersion, nodeDataBytes)
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
			} else {
				for _, rec := range record {
					var nodeData proto.Node
					err = nodeData.UnmarshalVT(rec.Data)
					if err != nil {
						ndw.Config.Log.Error().Err(err).Msg("Error unmarshaling node data")
					} else {
						hostname, err := os.Hostname()
						if err != nil {
							ndw.Config.Log.Error().Err(err).Msg("Error getting hostname")
						}
						// ignore our own node data
						if nodeData.Hostname == hostname {
							continue
						}

						ndw.Config.Log.Debug().Msgf("Received node data: %+v", &nodeData)

						// Insert or update node data in the database
						_, err = ndw.Config.DB.CreateMeshNode(context.Background(), models.CreateMeshNodeParams{
							MacAddr:  nodeData.Mac,
							IpAddr:   nodeData.Ipaddr,
							Hostname: nodeData.Hostname,
						})

						if err != nil {
							ndw.Config.Log.Error().Err(err).Msg("Error inserting node data into database")
						}
					}
				}
			}
		}
	}
}
