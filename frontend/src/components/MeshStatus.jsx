// =============================================================================
// MeshStatus.jsx — Mesh network status panel with sparkline graphs
// =============================================================================

import React from 'react';
import Sparkline from './Sparkline.jsx';

function signalToColor(dBm) {
  if (dBm > -60) return '#00e676';
  if (dBm > -75) return '#ffb300';
  return '#ff3b4d';
}

export default React.memo(function MeshStatusPanel({ data, neighborHistory }) {
  if (!data) {
    return (
      <div className="card">
        <div className="card-title">Mesh Status</div>
        <div style={{ fontSize: '0.82em' }}>
          <span className="status-dot dot-yellow" /> Connecting...
        </div>
      </div>
    );
  }

  const { status, nodes, neighbors, interfaces } = data;

  let dotClass = 'status-dot dot-yellow';
  let connText = 'Connecting...';
  if (status) {
    dotClass = status.connected ? 'status-dot dot-green' : 'status-dot dot-red';
    connText = status.connected ? 'Connected' : 'Disconnected';
  }

  return (
    <div className="card">
      <div className="card-title">Mesh Status</div>
      <div style={{ fontSize: '0.82em' }}>
        {/* Summary row */}
        <div style={{ display: 'flex', gap: '12px', flexWrap: 'wrap', marginBottom: '6px' }}>
          <span>
            <span className={dotClass} /> <span>{connText}</span>
          </span>
          <span style={{ color: 'var(--muted)' }}>
            Neighbors: <span>{status ? status.neighbors : '-'}</span>
          </span>
          <span style={{ color: 'var(--muted)' }}>
            Interfaces: <span>{status ? status.mesh_interfaces : '-'}</span>
          </span>
        </div>

        {/* Gateway indicator */}
        {status && status.is_gateway && (
          <div style={{ fontSize: '0.8em', color: 'var(--muted)' }}>Gateway mode active</div>
        )}

        {/* Nodes section — only show nodes that are currently reachable */}
        <div style={{ marginTop: '6px' }}>
          <div style={{ fontSize: '0.75em', color: 'var(--muted)', textTransform: 'uppercase', marginBottom: '4px' }}>
            Active Nodes
          </div>
          <div style={{ fontSize: '0.8em', maxHeight: '80px', overflowY: 'auto' }}>
            {(() => {
              // Build a set of neighbor hostnames/MACs for matching
              const neighborNames = new Set();
              if (neighbors && Array.isArray(neighbors)) {
                neighbors.forEach((nb) => {
                  if (nb.name) neighborNames.add(nb.name.split('_')[0]);
                  if (nb.mac) neighborNames.add(nb.mac);
                });
              }
              // Filter nodes to only those with a matching neighbor
              const activeNodes = (nodes && Array.isArray(nodes))
                ? nodes.filter((n) => {
                    if (neighborNames.has(n.hostname)) return true;
                    // Also check if any neighbor name starts with the hostname
                    for (const name of neighborNames) {
                      if (name.startsWith(n.hostname) || n.hostname.startsWith(name)) return true;
                    }
                    return false;
                  })
                : [];
              if (activeNodes.length === 0) {
                return <span style={{ color: 'var(--muted)' }}>No active nodes</span>;
              }
              return activeNodes.map((n, i) => (
                <div key={i} style={{ display: 'flex', gap: '8px', padding: '1px 0' }}>
                  <span style={{ color: 'var(--green)' }}>{n.hostname || '?'}</span>
                  <span style={{ color: 'var(--muted)' }}>{n.ip || ''}</span>
                </div>
              ));
            })()}
          </div>
        </div>

        {/* Neighbors section */}
        <div style={{ marginTop: '6px' }}>
          <div style={{ fontSize: '0.75em', color: 'var(--muted)', textTransform: 'uppercase', marginBottom: '4px' }}>
            Neighbors
          </div>
          <div style={{ fontSize: '0.8em', maxHeight: '120px', overflowY: 'auto' }}>
            {neighbors && Array.isArray(neighbors) && neighbors.length > 0 ? (
              neighbors.map((n, i) => {
                const key = n.name || n.mac;
                const hist = neighborHistory && neighborHistory[key];
                return (
                  <div key={i} style={{ display: 'flex', gap: '8px', padding: '2px 0', alignItems: 'center', flexWrap: 'wrap' }}>
                    <span>{key}</span>
                    <span style={{ color: 'var(--muted)' }}>{n.signal}dBm</span>
                    <span style={{ color: 'var(--muted)' }}>
                      {n.throughput ? (n.throughput / 1000000).toFixed(1) + ' Mbps' : ''}
                    </span>
                    {hist && hist.signal.length > 1 && (
                      <Sparkline
                        data={hist.signal}
                        color={signalToColor(n.signal || -100)}
                        min={-100}
                        max={-30}
                        width={50}
                        height={14}
                      />
                    )}
                    {hist && hist.throughput.length > 1 && (
                      <Sparkline
                        data={hist.throughput}
                        color="#00e5ff"
                        width={50}
                        height={14}
                      />
                    )}
                  </div>
                );
              })
            ) : (
              <span style={{ color: 'var(--muted)' }}>No neighbors</span>
            )}
          </div>
        </div>

        {/* Radios section */}
        <div style={{ marginTop: '6px' }}>
          <div style={{ fontSize: '0.75em', color: 'var(--muted)', textTransform: 'uppercase', marginBottom: '4px' }}>
            Radios
          </div>
          <div style={{ fontSize: '0.8em' }}>
            {interfaces && Array.isArray(interfaces) && interfaces.length > 0 ? (
              interfaces.map((r, i) => (
                <div key={i} style={{ display: 'flex', gap: '8px', padding: '1px 0' }}>
                  <span style={{ color: 'var(--green)' }}>{r.name}</span>
                  <span style={{ color: 'var(--muted)' }}>{r.frequency}MHz</span>
                  <span style={{ color: 'var(--muted)' }}>{r.channel_width}MHz</span>
                  <span style={{ color: 'var(--muted)' }}>{r.type}</span>
                </div>
              ))
            ) : (
              <span style={{ color: 'var(--muted)' }}>No radios</span>
            )}
          </div>
        </div>
      </div>
    </div>
  );
});
