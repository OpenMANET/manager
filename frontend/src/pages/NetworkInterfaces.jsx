// =============================================================================
// NetworkInterfaces.jsx — Network interfaces and DHCP settings
// =============================================================================

import { useState, useEffect } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { NetworkInterfaceService } from "../gen/openmanet/network_interface/v1/network_interface_service_connect.js";

const netClient = createClient(NetworkInterfaceService, transport);

const IFACE_TYPE_LABELS = { 0: '?', 1: 'Bridge', 2: 'Ethernet', 3: 'WiFi AP', 4: 'HaLow Mesh', 5: 'Batman', 6: 'Loopback', 7: 'VXLAN' };
const IFACE_STATUS_LABELS = { 0: '?', 1: 'Up', 2: 'Down' };

function formatBytes(bytes) {
  if (!bytes || bytes === 0n && typeof bytes === 'bigint') return '0 B';
  const n = Number(bytes);
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1024 * 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + ' MB';
  return (n / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
}

// ── Network Interfaces Section ──────────────────────────────────────────────
function InterfacesCard() {
  const [interfaces, setInterfaces] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    netClient.listNetworkInterfaces({})
      .then(resp => { setInterfaces(resp.interfaces ?? []); setError(null); })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="card" style={{ gridColumn: '1 / -1' }}>
      <div className="card-title">Network Interfaces</div>
      {error && <div style={{ color: 'var(--red)', fontSize: '0.82em', marginBottom: 6 }}>{error}</div>}
      {loading ? (
        <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>Loading...</div>
      ) : interfaces.length === 0 ? (
        <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>No interfaces found.</div>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.82em' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)', color: 'var(--muted)' }}>
                <th style={{ textAlign: 'left', padding: '4px 8px' }}>Name</th>
                <th style={{ textAlign: 'left', padding: '4px 8px' }}>Type</th>
                <th style={{ textAlign: 'center', padding: '4px 8px' }}>Status</th>
                <th style={{ textAlign: 'left', padding: '4px 8px' }}>IP</th>
                <th style={{ textAlign: 'left', padding: '4px 8px' }}>MAC</th>
                <th style={{ textAlign: 'right', padding: '4px 8px' }}>RX</th>
                <th style={{ textAlign: 'right', padding: '4px 8px' }}>TX</th>
                <th style={{ textAlign: 'right', padding: '4px 8px' }}>MTU</th>
              </tr>
            </thead>
            <tbody>
              {interfaces.map((iface, i) => (
                <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                  <td style={{ padding: '4px 8px', fontWeight: 600 }}>{iface.name}</td>
                  <td style={{ padding: '4px 8px' }}>{IFACE_TYPE_LABELS[iface.type] || '?'}</td>
                  <td style={{ padding: '4px 8px', textAlign: 'center' }}>
                    <span style={{
                      display: 'inline-block', padding: '1px 6px', borderRadius: 4, fontSize: '0.85em',
                      background: iface.status === 1 ? 'rgba(107,142,35,0.15)' : 'rgba(204,51,51,0.1)',
                      color: iface.status === 1 ? 'var(--green)' : 'var(--red)',
                    }}>
                      {IFACE_STATUS_LABELS[iface.status] || '?'}
                    </span>
                  </td>
                  <td style={{ padding: '4px 8px', fontFamily: 'monospace', fontSize: '0.9em' }}>{iface.ipAddress || '-'}</td>
                  <td style={{ padding: '4px 8px', fontFamily: 'monospace', fontSize: '0.9em' }}>{iface.macAddress || '-'}</td>
                  <td style={{ padding: '4px 8px', textAlign: 'right' }}>{formatBytes(iface.rxBytes)}</td>
                  <td style={{ padding: '4px 8px', textAlign: 'right' }}>{formatBytes(iface.txBytes)}</td>
                  <td style={{ padding: '4px 8px', textAlign: 'right' }}>{iface.mtu || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ── DHCP Section ────────────────────────────────────────────────────────────
function DHCPCard() {
  const [config, setConfig] = useState(null);
  const [activeLeases, setActiveLeases] = useState([]);
  const [staticLeases, setStaticLeases] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showActive, setShowActive] = useState(false);
  const [showStatic, setShowStatic] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    Promise.allSettled([
      netClient.getDHCPServerConfig({}),
      netClient.listActiveDHCPLeases({}),
      netClient.listStaticDHCPLeases({}),
    ]).then(([cfgRes, activeRes, staticRes]) => {
      if (cfgRes.status === 'fulfilled') setConfig(cfgRes.value.config);
      if (activeRes.status === 'fulfilled') setActiveLeases(activeRes.value.leases ?? []);
      if (staticRes.status === 'fulfilled') setStaticLeases(staticRes.value.leases ?? []);
      if (cfgRes.status === 'rejected') setError(cfgRes.reason?.message);
    }).finally(() => setLoading(false));
  }, []);

  return (
    <div className="card" style={{ gridColumn: '1 / -1' }}>
      <div className="card-title">DHCP Server</div>
      {error && <div style={{ color: 'var(--red)', fontSize: '0.82em', marginBottom: 6 }}>{error}</div>}
      {loading ? (
        <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>Loading...</div>
      ) : !config ? (
        <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>DHCP server not configured.</div>
      ) : (
        <>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8, fontSize: '0.85em', marginBottom: 10 }}>
            <div><span style={{ color: 'var(--muted)' }}>Interface:</span> {config.interfaceName}</div>
            <div><span style={{ color: 'var(--muted)' }}>Range:</span> {config.rangeStart} - {config.rangeEnd}</div>
            <div><span style={{ color: 'var(--muted)' }}>Lease Time:</span> {config.leaseTime}</div>
            <div><span style={{ color: 'var(--muted)' }}>DNS Fwd:</span> {config.dnsForwardingEnabled ? 'Yes' : 'No'}</div>
            <div><span style={{ color: 'var(--muted)' }}>Active Leases:</span> {config.activeLeaseCount}</div>
          </div>

          {/* Active leases */}
          <div style={{ marginBottom: 8 }}>
            <div onClick={() => setShowActive(!showActive)} style={{
              cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6,
              fontSize: '0.78em', color: 'var(--muted)',
            }}>
              <span style={{ fontSize: '0.75em' }}>{showActive ? '\u25BC' : '\u25B6'}</span>
              Active Leases ({activeLeases.length})
            </div>
            {showActive && activeLeases.length > 0 && (
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.8em', marginTop: 4 }}>
                <thead>
                  <tr style={{ borderBottom: '1px solid var(--border)', color: 'var(--muted)' }}>
                    <th style={{ textAlign: 'left', padding: '3px 6px' }}>Hostname</th>
                    <th style={{ textAlign: 'left', padding: '3px 6px' }}>MAC</th>
                    <th style={{ textAlign: 'left', padding: '3px 6px' }}>IP</th>
                    <th style={{ textAlign: 'right', padding: '3px 6px' }}>Expires</th>
                  </tr>
                </thead>
                <tbody>
                  {activeLeases.map((l, i) => (
                    <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ padding: '3px 6px' }}>{l.hostname || '-'}</td>
                      <td style={{ padding: '3px 6px', fontFamily: 'monospace', fontSize: '0.9em' }}>{l.macAddress}</td>
                      <td style={{ padding: '3px 6px', fontFamily: 'monospace', fontSize: '0.9em' }}>{l.ipAddress}</td>
                      <td style={{ padding: '3px 6px', textAlign: 'right' }}>{l.expiresSeconds}s</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>

          {/* Static leases */}
          <div>
            <div onClick={() => setShowStatic(!showStatic)} style={{
              cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6,
              fontSize: '0.78em', color: 'var(--muted)',
            }}>
              <span style={{ fontSize: '0.75em' }}>{showStatic ? '\u25BC' : '\u25B6'}</span>
              Static Reservations ({staticLeases.length})
            </div>
            {showStatic && staticLeases.length > 0 && (
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.8em', marginTop: 4 }}>
                <thead>
                  <tr style={{ borderBottom: '1px solid var(--border)', color: 'var(--muted)' }}>
                    <th style={{ textAlign: 'left', padding: '3px 6px' }}>Hostname</th>
                    <th style={{ textAlign: 'left', padding: '3px 6px' }}>MAC</th>
                    <th style={{ textAlign: 'left', padding: '3px 6px' }}>IP</th>
                  </tr>
                </thead>
                <tbody>
                  {staticLeases.map((l, i) => (
                    <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ padding: '3px 6px' }}>{l.hostname || '-'}</td>
                      <td style={{ padding: '3px 6px', fontFamily: 'monospace', fontSize: '0.9em' }}>{l.macAddress}</td>
                      <td style={{ padding: '3px 6px', fontFamily: 'monospace', fontSize: '0.9em' }}>{l.ipAddress}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// ── Main Page ───────────────────────────────────────────────────────────────
export default function NetworkInterfacesPage() {
  return (
    <div style={{ width: '100%', maxWidth: 900 }}>
      <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>Network</h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 8 }}>
        <InterfacesCard />
        <DHCPCard />
      </div>
    </div>
  );
}
