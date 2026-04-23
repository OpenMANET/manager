// =============================================================================
// Dashboard.jsx — Mesh operator overview
// =============================================================================
//
// Fixed-grid tactical dashboard:
//   topbar: NODE id + MESH / GPS / BLOS chips + local-timezone clock
//   row 1:  3 KPI panels — Mesh Peers · Link Quality 5m · Battery & Power
//   row 2:  Mesh Peers Live (col-span-3) · Alerts (col-span-1)
//   row 3:  System Resources (col-span-2) · Network Interfaces (col-span-2)
//
// PTT latency is intentionally not on this page — it lives on the Comms page
// with the rest of the realtime audio instrumentation. The full topology
// graph has its own /topology route; the dashboard deliberately does not
// embed it.

import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { DashboardService } from "../gen/openmanet/dashboard/v1/dashboard_service_connect.js";
import { NetworkInterfaceState } from "../gen/openmanet/dashboard/v1/dashboard_pb.js";
import { GNSSService } from "../gen/openmanet/gnss/v1/gnss_service_connect.js";
import { BLOSService } from "../gen/openmanet/blos/v1/blos_service_connect.js";
import { fetchMeshStatus, fetchMeshTopology, fetchMeshTopologyDelta } from '../services/meshApi.js';
import './Dashboard.css';

const dashClient = createClient(DashboardService, transport);
const gnssClient = createClient(GNSSService, transport);
const blosClient = createClient(BLOSService, transport);

const DASH_POLL_MS = 5000;
const MESH_POLL_MS = 10000;
const CHIP_POLL_MS = 10000;
const CLOCK_TICK_MS = 1000;
const LQ_HISTORY_LEN = 60;
const NEIGHBOR_STALE_MS = 30_000;

// ── Formatting ─────────────────────────────────────────────────────────────

function formatUptime(dur) {
  if (!dur) return '—';
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

function formatBytes(bytes) {
  if (!bytes) return '0 B';
  const n = Number(bytes);
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatMbps(bps) {
  if (!bps || bps <= 0) return '—';
  const mbps = bps / 1_000_000;
  if (mbps < 0.1) return `${(bps / 1000).toFixed(0)} Kbps`;
  if (mbps < 10) return `${mbps.toFixed(2)} Mbps`;
  return `${mbps.toFixed(1)} Mbps`;
}

function formatLast(tsMs, nowMs) {
  if (!tsMs) return '—';
  const diff = Math.max(0, nowMs - tsMs);
  if (diff < 1000) return '<1s';
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`;
  return `${Math.floor(diff / 3_600_000)}h`;
}

// Formats the current time in the browser's local timezone — the frontend
// runs in the operator's browser, so this matches the host the operator is
// looking at. `timeZoneName: 'short'` produces a short label like "PDT",
// "EST", or "GMT+10" depending on the locale's conventions.
function clockLocal(now) {
  const time = now.toLocaleTimeString('en-GB', { hour12: false });
  const parts = new Intl.DateTimeFormat('en-US', { timeZoneName: 'short' })
    .formatToParts(now);
  const tz = parts.find((p) => p.type === 'timeZoneName')?.value || 'UTC';
  return `${time} ${tz}`;
}

// ── Signal / link quality ──────────────────────────────────────────────────

// Most 802.11 surveys map good/moderate/weak at −50/−70/−85 dBm.  Linear
// interpolation between −50 (excellent) and −90 (floor) gives a reasonable
// 0–100 % score for the link-quality KPI.
function signalToPct(dbm) {
  if (!Number.isFinite(dbm) || dbm === 0) return 0;
  if (dbm >= -50) return 100;
  if (dbm <= -90) return 0;
  return Math.round(((dbm + 90) / 40) * 100);
}

function sigBars(dbm) {
  const pct = signalToPct(dbm);
  // 5 bars: warn when only one bar, off when zero.
  const filled = Math.max(0, Math.min(5, Math.round(pct / 20)));
  return [0, 1, 2, 3, 4].map((i) => {
    if (i >= filled) return '';
    if (filled <= 1) return 'warn';
    return 'on';
  });
}

function tqBadge(tq) {
  if (!Number.isFinite(tq) || tq <= 0) return '';
  if (tq >= 200) return 'badge-ok';
  if (tq >= 100) return 'badge-ok';
  return 'badge-warn';
}

// ── Topology helpers ───────────────────────────────────────────────────────

// Return a 1-hop list of gateway candidates: nodes whose neighbors include
// self, which for now we approximate as the first node in the topology (the
// daemon emits self first).  A full BFS would be nicer but the current mesh
// rarely exceeds 3 hops and the table only needs a rough hop indicator.
function buildPeerRows(topology, neighbors, historyRef, nowMs) {
  if (!neighbors || neighbors.length === 0) return [];
  const neighborHostByMac = new Map();
  const topoNodes = topology?.nodes ?? [];
  const self = topoNodes[0];
  const selfMac = self?.primaryMac?.toLowerCase() ?? '';

  // Build an adjacency map for hop-depth BFS rooted at self.
  const adj = new Map();
  for (const node of topoNodes) {
    const from = (node.primaryMac || '').toLowerCase();
    if (!adj.has(from)) adj.set(from, new Set());
    for (const e of node.neighbors || []) {
      const to = (e.neighborMac || '').toLowerCase();
      if (!to) continue;
      adj.get(from).add(to);
      if (!adj.has(to)) adj.set(to, new Set());
      adj.get(to).add(from);
      neighborHostByMac.set(to, e.neighborHostname || '');
    }
  }
  const hops = new Map();
  if (selfMac) {
    hops.set(selfMac, 0);
    const q = [selfMac];
    while (q.length) {
      const cur = q.shift();
      const dist = hops.get(cur);
      for (const nxt of adj.get(cur) || []) {
        if (!hops.has(nxt)) {
          hops.set(nxt, dist + 1);
          q.push(nxt);
        }
      }
    }
  }

  // Build TQ lookup keyed by neighbor MAC.
  const tqByMac = new Map();
  for (const node of topoNodes) {
    for (const e of node.neighbors || []) {
      const mac = (e.neighborMac || '').toLowerCase();
      if (!mac) continue;
      const prev = tqByMac.get(mac) ?? 0;
      if ((e.metric ?? 0) > prev) tqByMac.set(mac, e.metric);
    }
  }

  return neighbors.map((n) => {
    const mac = (n.mac || '').toLowerCase();
    const hist = historyRef.current[n.name || n.mac];
    const lastSeenMs = hist?.lastSeenMs ?? nowMs;
    const tq = tqByMac.get(mac) ?? 0;
    const hopCount = hops.get(mac) ?? 1;
    const hostname = neighborHostByMac.get(mac) || n.name || mac.slice(-5);
    return {
      key: n.mac || n.name,
      name: hostname,
      mac: n.mac || '',
      hops: hopCount || 1,
      tq,
      rssi: n.signal ?? 0,
      sig: sigBars(n.signal),
      lastMs: lastSeenMs,
    };
  }).sort((a, b) => a.hops - b.hops || (b.tq - a.tq));
}

// ── Alerts ─────────────────────────────────────────────────────────────────

function classifyAlerts({ mesh, peerRows, delta }) {
  const out = [];
  if (mesh?.status?.connected) {
    out.push({ level: 'ok', text: 'MESH UP · CONVERGED' });
  } else {
    out.push({ level: 'crit', text: 'MESH DOWN · NO NEIGHBORS' });
  }
  for (const p of peerRows) {
    if (p.tq > 0 && p.tq < 100) {
      out.push({ level: 'warn', text: `PEER ${p.name.toUpperCase()} TQ ${p.tq} · DEGRADED` });
    }
  }
  if (delta?.routesLost > 0) {
    out.push({ level: 'warn', text: `${delta.routesLost} ROUTE${delta.routesLost > 1 ? 'S' : ''} LOST · 60s` });
  }
  if (delta?.routesAdded > 0 && (delta?.routesLost ?? 0) === 0) {
    out.push({ level: 'ok', text: `MESH HEALED · +${delta.routesAdded} ROUTES` });
  }
  return out.slice(0, 6);
}

// ── Network interface helpers ──────────────────────────────────────────────

function inferRole(iface) {
  const name = (iface.interfaceName || '').toLowerCase();
  if (name.startsWith('eth') || name.startsWith('wwan')) return 'uplink';
  if (name === 'bat0' || name.startsWith('bat')) return 'mesh';
  if (name.startsWith('phy')) return 'mesh radio';
  if (name.startsWith('wlan')) return 'wlan';
  if (name.startsWith('tailscale')) return 'BLOS';
  if (name.startsWith('br-')) return 'bridge';
  return '—';
}

function stateBadge(state) {
  if (state === NetworkInterfaceState.CONNECTED) return { cls: 'badge-ok', dot: 'ok', label: 'UP' };
  if (state === NetworkInterfaceState.DISCONNECTED) return { cls: 'badge-crit', dot: 'crit', label: 'DOWN' };
  return { cls: 'badge-warn', dot: 'warn', label: 'IDLE' };
}

// Pull an address from the free-form `detail` field. The daemon formats
// connected interfaces like "10.41.25.72/16", "Connected — 3 neighbors",
// or "100.64.0.16/32" — the CIDR extractor finds the first two.
function extractAddr(detail) {
  if (!detail) return '—';
  const m = detail.match(/\d+\.\d+\.\d+\.\d+(\/\d+)?/);
  return m ? m[0] : detail.split('—')[0].trim() || detail;
}

// ── Main ───────────────────────────────────────────────────────────────────

export default function DashboardPage() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [meshData, setMeshData] = useState(null);
  const [topology, setTopology] = useState(null);
  const [delta, setDelta] = useState(null);
  const [gps, setGps] = useState(null);
  const [blosPeers, setBlosPeers] = useState(0);
  const [lqHistory, setLqHistory] = useState([]);
  const [now, setNow] = useState(() => Date.now());

  const neighborHistoryRef = useRef({});
  const lqHistoryRef = useRef([]);

  // ─ Polls ─
  const fetchStatus = useCallback(async () => {
    try {
      const resp = await dashClient.getDashboardStatus({});
      setData(resp);
    } catch {
      // best-effort
    } finally {
      setLoading(false);
    }
  }, []);

  const pollMesh = useCallback(async () => {
    try {
      const [md, topo, d] = await Promise.all([
        fetchMeshStatus(),
        fetchMeshTopology(),
        fetchMeshTopologyDelta(60),
      ]);
      setMeshData(md);
      setTopology(topo);
      setDelta(d);

      const nowMs = Date.now();
      if (Array.isArray(md?.neighbors)) {
        let sumPct = 0;
        let count = 0;
        for (const n of md.neighbors) {
          const key = n.name || n.mac;
          if (!key) continue;
          if (!neighborHistoryRef.current[key]) {
            neighborHistoryRef.current[key] = { lastSeenMs: nowMs };
          }
          neighborHistoryRef.current[key].lastSeenMs = nowMs;
          if (Number.isFinite(n.signal) && n.signal !== 0) {
            sumPct += signalToPct(n.signal);
            count += 1;
          }
        }
        // Prune peers we haven't seen for a while so old entries don't
        // anchor the peer list.
        for (const [key, h] of Object.entries(neighborHistoryRef.current)) {
          if (nowMs - h.lastSeenMs > NEIGHBOR_STALE_MS * 10) {
            delete neighborHistoryRef.current[key];
          }
        }
        if (count > 0) {
          const sample = Math.round(sumPct / count);
          lqHistoryRef.current.push(sample);
          if (lqHistoryRef.current.length > LQ_HISTORY_LEN) {
            lqHistoryRef.current.shift();
          }
          setLqHistory([...lqHistoryRef.current]);
        }
      }
    } catch {
      // best-effort
    }
  }, []);

  const pollChips = useCallback(async () => {
    try {
      const g = await gnssClient.getGNSSStatus({});
      setGps(g);
    } catch {
      // GNSS service may be unavailable; chip shows NO FIX
    }
    try {
      const b = await blosClient.listBLOSPeers({});
      setBlosPeers(b?.peers?.length ?? 0);
    } catch {
      // BLOS service may be disabled
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    const id = setInterval(fetchStatus, DASH_POLL_MS);
    return () => clearInterval(id);
  }, [fetchStatus]);

  useEffect(() => {
    pollMesh();
    const id = setInterval(pollMesh, MESH_POLL_MS);
    return () => clearInterval(id);
  }, [pollMesh]);

  useEffect(() => {
    pollChips();
    const id = setInterval(pollChips, CHIP_POLL_MS);
    return () => clearInterval(id);
  }, [pollChips]);

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), CLOCK_TICK_MS);
    return () => clearInterval(id);
  }, []);

  // ─ Derived ─
  const neighbors = useMemo(() => meshData?.neighbors ?? [], [meshData]);
  const peerRows = useMemo(
    () => buildPeerRows(topology, neighbors, neighborHistoryRef, now),
    [topology, neighbors, now],
  );

  const peerCount = peerRows.length;
  // Gateway = the batman-adv-selected best gateway reported by
  // StatusService.selected_gateway_mac. We cross-reference the MAC against
  // the mesh topology node list to surface a hostname; when the remote
  // hostname isn't known we render the short MAC. Self takes precedence
  // only when this node itself is the gateway in server mode.
  const gatewayName = useMemo(() => {
    if (meshData?.status?.is_gateway) return (data?.deviceInfo?.hostname || 'SELF').toUpperCase();
    const mac = (meshData?.status?.selected_gateway_mac || '').toLowerCase();
    if (!mac) return '—';
    for (const node of topology?.nodes ?? []) {
      if ((node.primaryMac || '').toLowerCase() === mac) {
        return (node.primaryHostname || node.primaryMac.slice(9)).toUpperCase();
      }
      for (const e of node.neighbors ?? []) {
        if ((e.neighborMac || '').toLowerCase() === mac) {
          return (e.neighborHostname || e.neighborMac.slice(9)).toUpperCase();
        }
      }
    }
    // Fall back to the short MAC if the gateway isn't represented in the
    // topology snapshot yet (e.g. we saw batctl gwj before batctl vd).
    return mac.slice(9).toUpperCase();
  }, [meshData, data, topology]);

  const hopsAvg = useMemo(() => {
    if (peerRows.length === 0) return '—';
    const sum = peerRows.reduce((a, p) => a + p.hops, 0);
    return (sum / peerRows.length).toFixed(1);
  }, [peerRows]);

  const throughputTotal = useMemo(() => {
    const bps = (neighbors || []).reduce((a, n) => a + (n.throughput || 0), 0);
    return formatMbps(bps);
  }, [neighbors]);

  const linkQuality = lqHistory.length > 0 ? lqHistory[lqHistory.length - 1] : 0;
  const lqClass = linkQuality >= 80 ? 'ok' : linkQuality >= 50 ? '' : 'warn';

  const alerts = useMemo(
    () => classifyAlerts({ mesh: meshData, peerRows, delta }),
    [meshData, peerRows, delta],
  );

  const interfaces = data?.networkSummary?.entries ?? [];

  // ─ Topbar state ─
  const hostname = data?.deviceInfo?.hostname || 'NODE';
  const primaryIp = useMemo(() => {
    const entries = data?.networkSummary?.entries ?? [];
    for (const e of entries) {
      if (e.state !== NetworkInterfaceState.CONNECTED) continue;
      const addr = extractAddr(e.detail);
      if (addr && addr !== '—') return addr;
    }
    return '—';
  }, [data]);

  const gpsFix = (gps?.position?.fixType ?? 0) >= 2; // 2D or 3D
  const gpsSats = gps?.satelliteStatus?.satellitesUsed ?? 0;
  const meshUp = !!meshData?.status?.connected;

  if (loading) {
    return <div className="dashboard-loading">Loading dashboard...</div>;
  }

  return (
    <>
      <div className="lat-topbar">
        <div className="node-id">
          {hostname.toUpperCase()}
          <span className="ip">{primaryIp}</span>
        </div>
        <div className="chips">
          <span className={`lat-chip ${meshUp ? 'ok' : 'crit'}`}>
            <span className="dot" /> MESH {meshUp ? 'UP' : 'DOWN'}
          </span>
          <span className={`lat-chip ${gpsFix ? 'ok' : 'warn'}`}>
            <span className="dot" /> GPS {gpsFix ? `LOCK · ${gpsSats} SATS` : 'NO FIX'}
          </span>
          <span className={`lat-chip ${blosPeers > 0 ? 'ok' : 'warn'}`}>
            <span className="dot" /> BLOS · {blosPeers} PEERS
          </span>
          {/* TODO(api-plan): battery percent — add BATT chip when backend exposes it */}
          <span className="dash-clock">{clockLocal(new Date(now))}</span>
        </div>
      </div>

      <div className="lat-view-header">
        <div>
          <h2>◇ Dashboard</h2>
          <div className="crumb">Overview · Live telemetry · {(DASH_POLL_MS / 1000).toFixed(0)}s refresh</div>
        </div>
        <div className="lat-view-toolbar">
          {/* TODO: wire export / customize actions */}
          <button className="lat-btn ghost" type="button">EXPORT</button>
          <button className="lat-btn" type="button">CUSTOMIZE</button>
        </div>
      </div>

      <div className="lat-body grid-4">
        {/* Row 1: 3 KPIs — PTT Latency lives on Comms, not here. */}
        <div className="dashboard-kpi-row">
          <div className="lat-panel">
            <div className="panel-head"><h3>Mesh Peers</h3></div>
            <div className="big-num">{peerCount}<span className="unit">nodes</span></div>
            <div className="kv"><span className="k">Gateway</span><span className="v accent">{gatewayName}</span></div>
            <div className="kv"><span className="k">Hops avg</span><span className="v">{hopsAvg}</span></div>
            <div className="kv"><span className="k">Throughput</span><span className="v">{throughputTotal}</span></div>
          </div>
          <div className="lat-panel">
            <div className="panel-head"><h3>Link Quality · 5m</h3></div>
            <div className={`big-num ${lqClass}`}>{linkQuality}<span className="unit">%</span></div>
            <div className={`spark${lqClass === 'warn' ? ' warn' : ''}`}>
              {lqHistory.length === 0 ? (
                <span style={{ height: '2%' }} />
              ) : (
                lqHistory.map((v, i) => (
                  <span key={i} style={{ height: `${Math.max(2, v)}%` }} />
                ))
              )}
            </div>
          </div>
          <div className="lat-panel">
            <div className="panel-head"><h3>Battery & Power</h3></div>
            {/* TODO(api-plan): surface battery metrics — percent, voltage, draw, eta */}
            <div className="big-num">—</div>
            <div className="kv"><span className="k">Voltage</span><span className="v">—</span></div>
            <div className="kv"><span className="k">Draw</span><span className="v">—</span></div>
            <div className="kv"><span className="k">ETA</span><span className="v">—</span></div>
          </div>
        </div>

        {/* Row 2: peers table + alerts */}
        <div className="lat-panel col-span-3">
          <div className="panel-head">
            <h3>Mesh Peers · Live</h3>
            <div className="actions">
              {/* TODO: wire table filter/sort/export */}
              <button type="button">FILTER</button>
              <button type="button">SORT</button>
              <button type="button">EXPORT</button>
            </div>
          </div>
          <div className="table-scroll">
            <table className="lat-table">
              <thead>
                <tr>
                  <th>Node</th><th>MAC</th><th>Hops</th><th>TQ</th>
                  <th>RSSI</th><th>Sig</th><th>Last</th>
                </tr>
              </thead>
              <tbody>
                {peerRows.length === 0 && (
                  <tr><td colSpan={7} className="mono">No neighbors reporting</td></tr>
                )}
                {peerRows.map((p) => (
                  <tr key={p.key}>
                    <td>{p.name}</td>
                    <td className="mono">{p.mac || '—'}</td>
                    <td>{p.hops}</td>
                    <td className={tqBadge(p.tq)}>{p.tq > 0 ? p.tq : '—'}</td>
                    <td>{p.rssi ? `${p.rssi}` : '—'}</td>
                    <td>
                      <div className="sig-bars">
                        {p.sig.map((cls, i) => (
                          <span key={i} className={cls} />
                        ))}
                      </div>
                    </td>
                    <td>{formatLast(p.lastMs, now)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        <div className="lat-panel">
          <div className="panel-head">
            <h3>Alerts · Active</h3>
            <div className="actions">
              {/* TODO: wire alert ack */}
              <button type="button">ACK ALL</button>
            </div>
          </div>
          {alerts.map((a, i) => (
            <div key={i} className={`lat-alert ${a.level}`}>{a.text}</div>
          ))}
        </div>

        {/* Row 3: system resources + interfaces */}
        <div className="lat-panel col-span-2">
          <div className="panel-head"><h3>System Resources</h3></div>
          <div className="dashboard-resources">
            <div className="dashboard-resources-bars">
              <PbarRow label="CPU" pct={data?.systemResources?.cpuLoadPercent ?? 0} />
              <PbarRow
                label="MEM"
                pct={memPct(data?.systemResources?.memoryUsedBytes, data?.systemResources?.memoryTotalBytes)}
                detail={`${formatBytes(data?.systemResources?.memoryUsedBytes)} / ${formatBytes(data?.systemResources?.memoryTotalBytes)}`}
              />
              <PbarRow
                label="OVERLAY"
                pct={memPct(data?.systemResources?.overlayUsedBytes, data?.systemResources?.overlayTotalBytes)}
                detail={`${formatBytes(data?.systemResources?.overlayUsedBytes)} / ${formatBytes(data?.systemResources?.overlayTotalBytes)}`}
              />
            </div>
            <div className="dashboard-resources-kv">
              <div className="kv"><span className="k">Uptime</span><span className="v accent">{formatUptime(data?.systemResources?.uptime)}</span></div>
              <div className="kv"><span className="k">Kernel</span><span className="v">{data?.deviceInfo?.kernel || '—'}</span></div>
              <div className="kv"><span className="k">Firmware</span><span className="v">{data?.deviceInfo?.firmware || '—'}</span></div>
              <div className="kv"><span className="k">Arch</span><span className="v">{data?.deviceInfo?.architecture || '—'}</span></div>
              {/* TODO(api-plan): expose CPU temperature via sysfs */}
              <div className="kv"><span className="k">Temp</span><span className="v">—</span></div>
            </div>
          </div>
        </div>

        <div className="lat-panel col-span-2">
          <div className="panel-head"><h3>Network Interfaces</h3></div>
          <div className="table-scroll">
            <table className="lat-table">
              <thead>
                <tr>
                  <th>Iface</th><th>Addr</th><th>State</th><th>Role</th><th>RX</th><th>TX</th>
                </tr>
              </thead>
              <tbody>
                {interfaces.length === 0 && (
                  <tr><td colSpan={6} className="mono">No network data</td></tr>
                )}
                {interfaces.map((iface) => {
                  const badge = stateBadge(iface.state);
                  return (
                    <tr key={iface.interfaceName}>
                      <td>
                        <span className={`dot-i ${badge.dot}`} />
                        {iface.interfaceName}
                      </td>
                      <td className="mono">{extractAddr(iface.detail)}</td>
                      <td className={badge.cls}>{badge.label}</td>
                      <td>{inferRole(iface)}</td>
                      {/* TODO(api-plan): expose per-interface RX/TX byte counters */}
                      <td>—</td>
                      <td>—</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>

      </div>
    </>
  );
}

// ── Small presentational helpers ──────────────────────────────────────────

function memPct(used, total) {
  const u = Number(used ?? 0);
  const t = Number(total ?? 0);
  if (t <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((u / t) * 100)));
}

function PbarRow({ label, pct, detail }) {
  const warn = pct >= 90 ? 'crit' : pct >= 70 ? 'warn' : '';
  return (
    <div className="pbar-row">
      <span className="pbar-label">{label}</span>
      <div className={`pbar ${warn}`.trim()}>
        <span style={{ width: `${pct}%` }} />
      </div>
      <span className="pbar-val">{detail || `${pct}%`}</span>
    </div>
  );
}
