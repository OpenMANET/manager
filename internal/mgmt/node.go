package mgmt

import (
	"os"
	"time"

	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	NodeDataType        uint8 = 102
	NodeDataTypeVersion uint8 = 1
)

type NodeDataWorker struct {
	Config       ManagementConfig
	Client       *alfred.Client
	Interval     time.Duration
	ShutdownChan <-chan os.Signal
}

func NewNodeDataWorker(config *ManagementConfig, client *alfred.Client, interval time.Duration, shutdownChan <-chan os.Signal) *NodeDataWorker {
	config.Log.Info().Msg("NodeDataWorker initialized")

	return &NodeDataWorker{
		Config:       *config,
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
			iface := network.GetInterfaceByName(ndw.Config.IFace)
			hostname, err := os.Hostname()
			if err != nil {
				ndw.Config.Log.Error().Err(err).Msg("Error getting hostname")
				hostname = "unknown"
			}

			nodeData := proto.Node{
				Mac:      iface.MAC,
				Hostname: hostname,
				Ipaddr:   iface.IP[0].IP.String(),
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
						ndw.Config.Log.Debug().Msgf("Received node data: %+v", &nodeData)
					}
				}
			}
		}
	}
}
