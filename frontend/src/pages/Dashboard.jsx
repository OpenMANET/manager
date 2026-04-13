// =============================================================================
// Dashboard.jsx — Device dashboard overview page
// =============================================================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { DashboardService } from "../gen/openmanet/dashboard/v1/dashboard_service_connect.js";
import { QuickAction, NetworkInterfaceState } from "../gen/openmanet/dashboard/v1/dashboard_pb.js";
import { fetchMeshStatus } from '../services/meshApi.js';
import MeshStatusPanel from '../components/MeshStatus.jsx';
import TopologyMap from '../components/TopologyMap.jsx';
import WidgetGrid from '../components/WidgetGrid.jsx';
import './Dashboard.css';

const dashClient = createClient(DashboardService, transport);
const POLL_INTERVAL = 5000;
const MESH_POLL_INTERVAL = 10000;
const NEIGHBOR_HISTORY_LENGTH = 30;

// ── Formatting helpers ──────────────────────────────────────────────────────

function formatUptime(dur) {
  if (!dur) return '-';
  const totalSec = Number(dur.seconds);
  if (totalSec <= 0) return '0m';
  const d = Math.floor(totalSec / 86400);
  const h = Math.floor((totalSec % 86400) / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const parts = [];
  if (d > 0) parts.push(`${d}d`);
  if (h > 0 || d > 0) parts.push(`${h}h`);
  parts.push(`${m}m`);
  return parts.join(' ');
}

function formatLocalTime(ts) {
  if (!ts) return '-';
  const d = new Date(Number(ts.seconds) * 1000 + Math.floor(ts.nanos / 1e6));
  if (isNaN(d.getTime())) return '-';
  return d.toISOString().replace('T', ' ').slice(0, 19) + ' UTC';
}

function formatBytes(bytes) {
  if (!bytes || bytes === 0n && typeof bytes === 'bigint') return '0 B';
  const n = Number(bytes);
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1024 * 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + ' MB';
  return (n / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
}

function barColor(pct) {
  if (pct >= 90) return 'var(--red)';
  if (pct >= 70) return 'var(--yellow)';
  return 'var(--green)';
}

function stateColor(state) {
  if (state === NetworkInterfaceState.CONNECTED) return 'var(--green)';
  if (state === NetworkInterfaceState.DISCONNECTED) return 'var(--red)';
  return 'var(--muted)';
}

function stateLabel(state) {
  if (state === NetworkInterfaceState.CONNECTED) return 'Connected';
  if (state === NetworkInterfaceState.DISCONNECTED) return 'Disconnected';
  return 'Not connected';
}

// ── Sub-components ──────────────────────────────────────────────────────────

const rowStyle = {
  display: 'flex', justifyContent: 'space-between', alignItems: 'center',
  padding: '6px 0', borderBottom: '1px solid var(--border)',
};
const labelStyle = { color: 'var(--muted)', fontSize: '0.88em' };
const valueStyle = { fontWeight: 600, fontSize: '0.88em', textAlign: 'right' };

function DeviceInfoCard({ info }) {
  const rows = [
    { label: 'Hostname', value: info?.hostname },
    { label: 'Model', value: info?.model },
    { label: 'Firmware', value: info?.firmware },
    { label: 'Kernel', value: info?.kernel },
    { label: 'Architecture', value: info?.architecture },
  ];
  return (
    <div className="card">
      <div className="card-title">Device Information</div>
      {rows.map((r) => (
        <div key={r.label} style={rowStyle}>
          <span style={labelStyle}>{r.label}</span>
          <span style={valueStyle}>{r.value || '-'}</span>
        </div>
      ))}
    </div>
  );
}

function ProgressBar({ label, value, total, formatFn }) {
  const pct = total > 0 ? (value / total) * 100 : 0;
  return (
    <div style={{ padding: '6px 0', borderBottom: '1px solid var(--border)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4, fontSize: '0.88em' }}>
        <span style={{ color: 'var(--muted)' }}>{label}</span>
        <span style={{ fontWeight: 600 }}>{formatFn ? `${formatFn(value)} / ${formatFn(total)}` : `${Math.round(pct)} % / 100 %`}</span>
      </div>
      <div className="dashboard-bar-track">
        <div className="dashboard-bar-fill" style={{ width: `${Math.min(pct, 100)}%`, background: barColor(pct) }} />
      </div>
    </div>
  );
}

function SystemResourcesCard({ resources }) {
  const cpu = resources?.cpuLoadPercent ?? 0;
  const memUsed = Number(resources?.memoryUsedBytes ?? 0);
  const memTotal = Number(resources?.memoryTotalBytes ?? 0);
  const ovlUsed = Number(resources?.overlayUsedBytes ?? 0);
  const ovlTotal = Number(resources?.overlayTotalBytes ?? 0);

  return (
    <div className="card">
      <div className="card-title">System Resources</div>
      <div style={rowStyle}>
        <span style={labelStyle}>Uptime</span>
        <span style={valueStyle}>{formatUptime(resources?.uptime)}</span>
      </div>
      <div style={rowStyle}>
        <span style={labelStyle}>Local Time</span>
        <span style={valueStyle}>{formatLocalTime(resources?.localTime)}</span>
      </div>
      <ProgressBar label="CPU Load" value={cpu} total={100} />
      <ProgressBar label="Memory" value={memUsed} total={memTotal} formatFn={formatBytes} />
      <ProgressBar label="Overlay" value={ovlUsed} total={ovlTotal} formatFn={formatBytes} />
    </div>
  );
}

function NetworkSummaryCard({ summary }) {
  const entries = summary?.entries ?? [];
  return (
    <div className="card">
      <div className="card-title">Network Summary</div>
      {entries.length === 0 && <div style={{ color: 'var(--muted)', fontSize: '0.85em', padding: '8px 0' }}>No network data</div>}
      {entries.map((e) => (
        <div key={e.interfaceName} style={{ ...rowStyle, gap: 8 }}>
          <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
            <span className="status-dot" style={{ background: stateColor(e.state), flexShrink: 0 }} />
            <span style={{ fontSize: '0.88em' }}>{e.displayName || e.interfaceName}</span>
          </span>
          <span style={{ fontSize: '0.88em', fontWeight: 600, textAlign: 'right', whiteSpace: 'nowrap',
            color: e.state === NetworkInterfaceState.DISCONNECTED ? 'var(--red)' : 'var(--text)' }}>
            {e.detail || stateLabel(e.state)}
          </span>
        </div>
      ))}
    </div>
  );
}

function QuickActionsCard({ onReboot, rebooting }) {
  return (
    <div className="card">
      <div className="card-title">Quick Actions</div>
      <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', paddingTop: 4 }}>
        <button
          onClick={onReboot}
          disabled={rebooting}
          style={{
            padding: '8px 20px', border: '1px solid var(--red)', borderRadius: 6,
            cursor: rebooting ? 'not-allowed' : 'pointer', fontSize: '0.85em', fontWeight: 600,
            background: 'rgba(204,51,51,0.15)', color: 'var(--red)', opacity: rebooting ? 0.5 : 1,
          }}
        >
          {rebooting ? 'Rebooting\u2026' : 'Reboot Device'}
        </button>
      </div>
    </div>
  );
}

// ── Widget configuration ───────────────────────────────────────────────────

const DASHBOARD_WIDGETS = [
  { id: 'deviceInfo',      label: 'Device Information', minWidth: 20, defaultWidth: 50 },
  { id: 'systemResources', label: 'System Resources',   minWidth: 20, defaultWidth: 50 },
  { id: 'networkSummary',  label: 'Network Summary',    minWidth: 20, defaultWidth: 50 },
  { id: 'meshStatus',      label: 'Mesh Status',        minWidth: 20, defaultWidth: 50 },
  { id: 'topologyMap',     label: 'Topology Map',       minWidth: 25, defaultWidth: 100 },
  { id: 'quickActions',    label: 'Quick Actions',      minWidth: 20, defaultWidth: 100 },
];

// ── Main page component ─────────────────────────────────────────────────────

export default function DashboardPage() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [rebooting, setRebooting] = useState(false);
  const [meshData, setMeshData] = useState(null);
  const [neighborHistory, setNeighborHistory] = useState({});
  const neighborHistoryRef = useRef({});
  const pollRef = useRef(null);
  const meshPollRef = useRef(null);

  const fetchStatus = useCallback(async () => {
    try {
      const resp = await dashClient.getDashboardStatus({});
      setData(resp);
      setLoading(false);
    } catch {
      setLoading(false);
    }
  }, []);

  const pollMesh = useCallback(async () => {
    try {
      const md = await fetchMeshStatus();
      setMeshData(md);
      if (md.neighbors && Array.isArray(md.neighbors)) {
        md.neighbors.forEach((n) => {
          const key = n.name || n.mac;
          if (!neighborHistoryRef.current[key]) {
            neighborHistoryRef.current[key] = { signal: [], throughput: [] };
          }
          const h = neighborHistoryRef.current[key];
          h.signal.push(n.signal);
          h.throughput.push(n.throughput || 0);
          if (h.signal.length > NEIGHBOR_HISTORY_LENGTH) {
            h.signal.shift();
            h.throughput.shift();
          }
        });
        setNeighborHistory({ ...neighborHistoryRef.current });
      }
    } catch {
      // mesh status is best-effort
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    pollRef.current = setInterval(fetchStatus, POLL_INTERVAL);
    return () => clearInterval(pollRef.current);
  }, [fetchStatus]);

  useEffect(() => {
    pollMesh();
    meshPollRef.current = setInterval(pollMesh, MESH_POLL_INTERVAL);
    return () => clearInterval(meshPollRef.current);
  }, [pollMesh]);

  const handleReboot = useCallback(async () => {
    if (!window.confirm('Are you sure you want to reboot this device? It will be temporarily unreachable.')) return;
    setRebooting(true);
    try {
      await dashClient.executeQuickAction({ action: QuickAction.REBOOT_DEVICE });
    } catch {
      // device may become unreachable immediately
    } finally {
      setTimeout(() => setRebooting(false), 5000);
    }
  }, []);

  const renderWidget = useCallback((id) => {
    switch (id) {
      case 'deviceInfo':      return <DeviceInfoCard info={data?.deviceInfo} />;
      case 'systemResources': return <SystemResourcesCard resources={data?.systemResources} />;
      case 'networkSummary':  return <NetworkSummaryCard summary={data?.networkSummary} />;
      case 'meshStatus':      return <MeshStatusPanel data={meshData} neighborHistory={neighborHistory} />;
      case 'topologyMap':     return <TopologyMap data={meshData} />;
      case 'quickActions':    return <QuickActionsCard onReboot={handleReboot} rebooting={rebooting} />;
      default:                return null;
    }
  }, [data, meshData, neighborHistory, handleReboot, rebooting]);

  if (loading) {
    return (
      <div style={{ width: '100%', maxWidth: '100%', color: 'var(--muted)', padding: '24px 0' }}>
        Loading dashboard...
      </div>
    );
  }

  return (
    <WidgetGrid
      pageId="dashboard"
      title="Dashboard"
      widgets={DASHBOARD_WIDGETS}
      renderWidget={renderWidget}
    />
  );
}
