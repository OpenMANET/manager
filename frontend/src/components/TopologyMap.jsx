// =============================================================================
// TopologyMap.jsx — Force-directed mesh topology visualization (reagraph)
// =============================================================================
// Renders the full mesh network as a force-directed 2D graph:
//   • One node per mesh peer (batman-adv primary MAC).
//   • One node per attached TT client, clustered with its parent peer.
//   • Edges = reported mesh links (batman-adv TQ); local edges colored by
//     signal strength in dBm when available.
//
// Data source: MeshTopologyService.GetMeshTopology via services/meshApi.js.

import React, { useMemo } from 'react';
import { GraphCanvas } from 'reagraph';
import { buildGraphData } from './topologyGraph.js';

export default React.memo(function TopologyMap({ topology }) {
  const { nodes, edges } = useMemo(() => buildGraphData(topology), [topology]);
  const hasData = nodes.length > 0;

  return (
    <div className="card span-2">
      <div className="card-title">Network Topology</div>
      <div
        data-testid="topology-map"
        style={{
          width: '100%',
          height: '260px',
          position: 'relative',
          background: '#111821',
          borderRadius: 4,
          overflow: 'hidden',
        }}
      >
        {hasData ? (
          <GraphCanvas
            nodes={nodes}
            edges={edges}
            layoutType="forceDirected2d"
            clusterAttribute="cluster"
            cameraMode="pan"
            labelType="auto"
            edgeLabelPosition="natural"
            draggable
          />
        ) : (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              color: '#9aa7b3',
              fontSize: 12,
              fontFamily: '-apple-system, sans-serif',
            }}
          >
            No topology data
          </div>
        )}
      </div>
    </div>
  );
});
