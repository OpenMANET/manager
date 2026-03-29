// =============================================================================
// meshApi.js — Mesh network status API polling
// =============================================================================
//
// Fetches mesh network status from the Go backend REST API.
//
// The backend exposes four endpoints that together give a complete picture
// of the mesh network state:
//   /api/status     — overall connection state, neighbor count, gateway flag
//   /api/nodes      — list of known mesh nodes (hostname, IP)
//   /api/neighbors  — list of direct neighbors (MAC, signal, throughput)
//   /api/interfaces — list of mesh radio interfaces (freq, channel width, type)
//
// We fetch all four in parallel using Promise.allSettled so that a failure on
// one endpoint doesn't block the others.  This is important because some
// endpoints may be unavailable during mesh reconfiguration.

// -----------------------------------------------------------------------------
// fetchMeshStatus()
// -----------------------------------------------------------------------------
// Fetches all mesh status endpoints in parallel.
//
// Returns an object with four keys (status, nodes, neighbors, interfaces).
// Each value is the parsed JSON response if the fetch succeeded, or null if
// it failed.  The caller is responsible for handling null values gracefully
// (e.g., showing "—" or "API error" in the UI).
export async function fetchMeshStatus() {
  const [statusRes, nodesRes, neighborsRes, ifacesRes] = await Promise.allSettled([
    fetch('/api/status').then((r) => r.json()),
    fetch('/api/nodes').then((r) => r.json()),
    fetch('/api/neighbors').then((r) => r.json()),
    fetch('/api/interfaces').then((r) => r.json()),
  ]);

  return {
    status: statusRes.status === 'fulfilled' ? statusRes.value : null,
    nodes: nodesRes.status === 'fulfilled' ? nodesRes.value : null,
    neighbors: neighborsRes.status === 'fulfilled' ? neighborsRes.value : null,
    interfaces: ifacesRes.status === 'fulfilled' ? ifacesRes.value : null,
  };
}
