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
import { MeshTopologyService } from "../gen/openmanet/mesh_topology/v1/mesh_topology_service_connect.js";

const statusClient = createClient(StatusService, transport);
const nodeClient = createClient(NodeService, transport);
const meshClient = createClient(MeshNeighborService, transport);
const ifaceClient = createClient(InterfaceService, transport);
const topologyClient = createClient(MeshTopologyService, transport);

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
        selected_gateway_mac: st?.selectedGatewayMac ?? '',
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

// -----------------------------------------------------------------------------
// fetchMeshTopology()
// -----------------------------------------------------------------------------
// Calls MeshTopologyService.GetMeshTopology and returns a plain-JS object
// suitable for the TopologyMap component. The wire format is one row per
// best-route entry from the local batman-adv originator table, enriched with
// bat-hosts hostnames:
//
//   {
//     selfMac, selfHostname, algorithm,            // who we are + batman version
//     collectedAt: Date | null,
//     originators: [
//       {
//         origMac, origHostname,                   // the reachable peer
//         nextHopMac, nextHopHostname,              // our forwarding target
//         hardIfname,                               // wlan0 | phy2-mesh0 | vxlan0 | …
//         tq, throughput,                           // BATMAN_IV TQ or BATMAN_V kbps
//         lastSeenMs, hops,
//       }
//     ],
//   }
//
// Returns null on failure. Callers treat null as "topology temporarily
// unavailable" and render accordingly.
export async function fetchMeshTopology() {
  try {
    const resp = await topologyClient.getMeshTopology({});
    const t = resp.topology;
    if (!t) return null;

    const collectedAt = t.collectedAt
      ? new Date(Number(t.collectedAt.seconds) * 1000)
      : null;

    return {
      selfMac: t.selfMac ?? '',
      selfHostname: t.selfHostname ?? '',
      algorithm: t.algorithm ?? '',
      collectedAt,
      originators: (t.originators ?? []).map((o) => ({
        origMac: o.origMac ?? '',
        origHostname: o.origHostname ?? '',
        nextHopMac: o.nextHopMac ?? '',
        nextHopHostname: o.nextHopHostname ?? '',
        hardIfname: o.hardIfname ?? '',
        tq: o.tq ?? 0,
        throughput: o.throughput ?? 0,
        lastSeenMs: o.lastSeenMs ?? 0,
        hops: o.hops ?? 0,
      })),
    };
  } catch {
    return null;
  }
}

// -----------------------------------------------------------------------------
// fetchMeshTopologyDelta()
// -----------------------------------------------------------------------------
// Calls MeshTopologyService.GetMeshTopologyDelta and returns the aggregated
// churn counters over the requested window (default 60s). Returns null when
// the call fails or the delta tracker has not yet collected enough samples
// (actualWindow <= 1s). Callers should render em-dashes when null is
// returned.
//
// Shape:
//   {
//     routesAdded: number,
//     routesLost: number,
//     gatewayChanges: number,
//     reconvergeMs: number,
//     actualWindowMs: number,
//   }
export async function fetchMeshTopologyDelta(windowSeconds = 60) {
  try {
    const resp = await topologyClient.getMeshTopologyDelta({
      window: { seconds: BigInt(windowSeconds), nanos: 0 },
    });

    const actualWindowMs = durationToMs(resp.actualWindow);
    if (actualWindowMs < 1000) return null;

    return {
      routesAdded: resp.routesAdded ?? 0,
      routesLost: resp.routesLost ?? 0,
      gatewayChanges: resp.gatewayChanges ?? 0,
      reconvergeMs: durationToMs(resp.reconverge),
      actualWindowMs,
    };
  } catch {
    return null;
  }
}

function durationToMs(d) {
  if (!d) return 0;
  const seconds = Number(d.seconds ?? 0);
  const nanos = Number(d.nanos ?? 0);
  return seconds * 1000 + Math.floor(nanos / 1_000_000);
}
