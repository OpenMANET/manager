// =============================================================================
// meshApi.js — Mesh network status via ConnectRPC
// =============================================================================
//
// Fetches mesh network status directly from the openmanetd ConnectRPC backend.
//
// Four service RPCs are called in parallel:
//   StatusService.GetServiceStatus  — connection state, neighbor count, gateway
//   NodeService.ListNodes           — known mesh nodes (hostname, IP)
//   MeshNeighborService.ListMeshNeighbors — direct neighbors (MAC, signal, throughput)
//   InterfaceService.ListWirelessInterfaces — mesh radio interfaces
//
// Responses are mapped to the JSON shape consumed by the UI components so that
// MeshStatus.jsx, TopologyMap.jsx, and App.jsx require no changes.

import { createClient } from "@connectrpc/connect";
import { transport } from "./connectClient.js";
import { StatusService } from "../gen/openmanet/service/v1/status_connect.js";
import { NodeService } from "../gen/openmanet/service/v1/node_connect.js";
import { MeshNeighborService } from "../gen/openmanet/service/v1/mesh_connect.js";
import { InterfaceService } from "../gen/openmanet/service/v1/interface_connect.js";

const statusClient = createClient(StatusService, transport);
const nodeClient = createClient(NodeService, transport);
const meshClient = createClient(MeshNeighborService, transport);
const ifaceClient = createClient(InterfaceService, transport);

// -----------------------------------------------------------------------------
// fetchMeshStatus()
// -----------------------------------------------------------------------------
// Fetches all mesh status RPCs in parallel.
//
// Returns an object with four keys (status, nodes, neighbors, interfaces).
// Each value is the mapped response if the call succeeded, or null if it
// failed.  The caller is responsible for handling null values gracefully.
export async function fetchMeshStatus() {
  const [statusRes, nodesRes, neighborsRes, ifacesRes] = await Promise.allSettled([
    statusClient.getServiceStatus({}).then((resp) => {
      const st = resp.status;
      return {
        connected: st?.isConnected ?? false,
        neighbors: st?.connectedNeighbors ?? 0,
        mesh_interfaces: st?.activeMeshInterfaces ?? 0,
        is_gateway: st?.isMeshGateway ?? false,
      };
    }),
    nodeClient.listNodes({}).then((resp) =>
      (resp.nodes ?? []).map((n) => ({
        hostname: n.hostname,
        ip: n.ipaddr,
      })),
    ),
    meshClient.listMeshNeighbors({}).then((resp) =>
      (resp.neighbors ?? []).map((n) => ({
        name: n.neighbor,
        mac: n.hardwareAddress,
        signal: n.signal,
        throughput: n.throughput,
      })),
    ),
    ifaceClient.listWirelessInterfaces({}).then((resp) =>
      (resp.interfaces ?? []).map((i) => ({
        name: i.name,
        type: i.interfaceType,
        frequency: i.frequency,
        channel_width: i.channelWidth,
      })),
    ),
  ]);

  return {
    status: statusRes.status === 'fulfilled' ? statusRes.value : null,
    nodes: nodesRes.status === 'fulfilled' ? nodesRes.value : null,
    neighbors: neighborsRes.status === 'fulfilled' ? neighborsRes.value : null,
    interfaces: ifacesRes.status === 'fulfilled' ? ifacesRes.value : null,
  };
}
