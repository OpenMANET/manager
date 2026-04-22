// =============================================================================
// Dashboard.jsx — Device dashboard overview page
// =============================================================================
//
// Widget grid shown at /. Cards rendered as Lattice panels via WidgetGrid's
// drag/resize container.  Order: Device Info · System Resources · Network ·
// Mesh Status · Topology Map · Quick Actions.

import { useState, useEffect, useCallback, useRef, Suspense, lazy } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { DashboardService } from "../gen/openmanet/dashboard/v1/dashboard_service_connect.js";
import { QuickAction, NetworkInterfaceState } from "../gen/openmanet/dashboard/v1/dashboard_pb.js";
import { fetchMeshStatus, fetchMeshTopology } from '../services/meshApi.js';
import MeshStatusPanel from '../components/MeshStatus.jsx';
import WidgetGrid from '../components/WidgetGrid.jsx';
import './Dashboard.css';

// TopologyMap pulls in reagraph + three.js (~900 KB). Lazy-loaded so the
// dashboard initial load doesn't include the WebGL graph bundle.
const TopologyMap = lazy(() => import('../components/TopologyMap.jsx'));

function TopologyMapPlaceholder() {
  return (
    <div className="lat-panel">
      <div className="panel-head"><h3>Network Topology</h3></div>
      <div className="dashboard-loading">Loading topology…</div>
    </div>
  );
}

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
  if (!bytes) return '0 B';
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

// Tests inspect the inline background color, so keep dot styling inline.
function stateDotStyle(state) {
  if (state === NetworkInterfaceState.CONNECTED) return { background: 'var(--green)' };
  if (state === NetworkInterfaceState.DISCONNECTED) return { background: 'var(--red)' };
  return { background: 'var(--muted)' };
}

function stateLabel(state) {
  if (state === NetworkInterfaceState.CONNECTED) return 'Connected';
  if (state === NetworkInterfaceState.DISCONNECTED) return 'Disconnected';
  return 'Not connected';
}

// ── Sub-components ──────────────────────────────────────────────────────────

function DeviceInfoCard({ info }) {
  const rows = [
    ['Hostname',     info?.hostname],
    ['Model',        info?.model],
    ['Firmware',     info?.firmware],
    ['Kernel',       info?.kernel],
    ['Architecture', info?.architecture],
  ];
  return (
    <div className="lat-panel">
      <div className="panel-head"><h3>Device Information</h3></div>
      {rows.map(([k, v]) => (
        <div key={k} className="kv">
          <span className="k">{k}</span>
          <span className="v">{v || '-'}</span>
        </div>
      ))}
    </div>
  );
}

function PBar({ label, value, total, formatFn }) {
  const pct = total > 0 ? (value / total) * 100 : 0;
  const pctClamped = Math.max(0, Math.min(100, pct));
  const detail = formatFn ? `${formatFn(value)} / ${formatFn(total)}` : `${Math.round(pct)} % / 100 %`;
  return (
    <div className="dashboard-pbar">
      <div className="dashboard-pbar-row">
        <span className="k">{label}</span>
        <span className="v">{detail}</span>
      </div>
      <div className="dashboard-bar-track">
        <div
          className="dashboard-bar-fill"
          style={{ width: `${pctClamped}%`, background: barColor(pctClamped) }}
        />
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
    <div className="lat-panel">
      <div className="panel-head"><h3>System Resources</h3></div>
      <div className="kv">
        <span className="k">Uptime</span>
        <span className="v accent">{formatUptime(resources?.uptime)}</span>
      </div>
      <div className="kv">
        <span className="k">Local Time</span>
        <span className="v">{formatLocalTime(resources?.localTime)}</span>
      </div>
      <PBar label="CPU Load" value={cpu} total={100} />
      <PBar label="Memory" value={memUsed} total={memTotal} formatFn={formatBytes} />
      <PBar label="Overlay" value={ovlUsed} total={ovlTotal} formatFn={formatBytes} />
    </div>
  );
}

function NetworkSummaryCard({ summary }) {
  const entries = summary?.entries ?? [];
  return (
    <div className="lat-panel">
      <div className="panel-head"><h3>Network Summary</h3></div>
      {entries.length === 0 && (
        <div className="dashboard-empty">No network data</div>
      )}
      {entries.map((e) => (
        <div key={e.interfaceName} className="kv">
          <span className="k" style={{ display: 'flex', alignItems: 'center' }}>
            <span className="status-dot" style={stateDotStyle(e.state)} />
            {e.displayName || e.interfaceName}
          </span>
          <span
            className={'v' + (e.state === NetworkInterfaceState.DISCONNECTED ? ' crit' : '')}
            title={e.detail || stateLabel(e.state)}
          >
            {e.detail || stateLabel(e.state)}
          </span>
        </div>
      ))}
    </div>
  );
}

function QuickActionsCard({ onReboot, rebooting }) {
  return (
    <div className="lat-panel">
      <div className="panel-head"><h3>Quick Actions</h3></div>
      <div className="dashboard-actions">
        <button
          className="lat-btn danger solid"
          onClick={onReboot}
          disabled={rebooting}
          type="button"
        >
          {rebooting ? 'Rebooting\u2026' : 'Reboot Device'}
        </button>
      </div>
    </div>
  );
}

// ── Widget configuration ───────────────────────────────────────────────────

const DASHBOARD_WIDGETS = [
  { id: 'deviceInfo',      label: 'Device',           minWidth: 20, defaultWidth: 33 },
  { id: 'systemResources', label: 'System Resources', minWidth: 20, defaultWidth: 33 },
  { id: 'networkSummary',  label: 'Network',          minWidth: 20, defaultWidth: 34 },
  { id: 'meshStatus',      label: 'Mesh Status',      minWidth: 30, defaultWidth: 66 },
  { id: 'quickActions',    label: 'Quick Actions',    minWidth: 20, defaultWidth: 34 },
  { id: 'topologyMap',     label: 'Topology Map',     minWidth: 30, defaultWidth: 100 },
];

// ── Main page component ─────────────────────────────────────────────────────

export default function DashboardPage() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [rebooting, setRebooting] = useState(false);
  const [meshData, setMeshData] = useState(null);
  const [meshTopology, setMeshTopology] = useState(null);
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
      const [md, topo] = await Promise.all([
        fetchMeshStatus(),
        fetchMeshTopology(),
      ]);
      setMeshData(md);
      setMeshTopology(topo);
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
      case 'topologyMap':     return (
        <Suspense fallback={<TopologyMapPlaceholder />}>
          <TopologyMap topology={meshTopology} />
        </Suspense>
      );
      case 'quickActions':    return <QuickActionsCard onReboot={handleReboot} rebooting={rebooting} />;
      default:                return null;
    }
  }, [data, meshData, meshTopology, neighborHistory, handleReboot, rebooting]);

  if (loading) {
    return <div className="dashboard-loading">Loading dashboard...</div>;
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
