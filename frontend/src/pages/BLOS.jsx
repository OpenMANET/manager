// =============================================================================
// BLOS.jsx — Beyond Line of Sight (VPN/Tailscale) configuration page
// =============================================================================
// The Lattice layout includes panels for traffic, DERP relay, overlay peers,
// ACL tags, and a keepalive log. All panels bind to data exposed by
// BLOSService (GetBLOSStatus / ListBLOSPeers / StreamBLOSEvents). When the
// BLOS subsystem is not running, the RPCs return zero-valued or empty
// structures and the UI renders em-dashes.

import { useState, useEffect, useCallback, useRef } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { BLOSService } from "../gen/openmanet/blos/v1/blos_service_connect.js";
import { BLOSEventKind } from "../gen/openmanet/blos/v1/blos_pb.js";
import './BLOS.css';

const blosClient = createClient(BLOSService, transport);

// Event-log cap. The keepalive panel shows the most recent N events — older
// entries are discarded so the panel never grows unbounded.
const MAX_EVENTS = 50;

const EVENT_KIND_LABELS = {
  [BLOSEventKind.BACKEND_STATE]: 'STATE',
  [BLOSEventKind.PEER_ADDED]: 'PEER+',
  [BLOSEventKind.PEER_LOST]: 'PEER-',
  [BLOSEventKind.PEER_ONLINE]: 'ONLINE',
  [BLOSEventKind.PEER_OFFLINE]: 'OFFLINE',
  [BLOSEventKind.DERP_CHANGED]: 'DERP',
  [BLOSEventKind.KEEPALIVE]: 'KA',
};

function formatKbps(bytesPerSec) {
  if (!bytesPerSec || bytesPerSec <= 0) return '—';
  const kbps = (bytesPerSec * 8) / 1000;
  if (kbps < 10) return kbps.toFixed(1);
  return Math.round(kbps).toString();
}

function formatBytes(n) {
  if (!n) return '—';
  const v = Number(n);
  if (v < 1024) return `${v} B`;
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KiB`;
  if (v < 1024 * 1024 * 1024) return `${(v / (1024 * 1024)).toFixed(1)} MiB`;
  return `${(v / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
}

function formatSince(ts) {
  if (!ts?.seconds) return '—';
  const ms = Number(ts.seconds) * 1000;
  const diff = Math.max(0, Date.now() - ms);
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

function formatLastSeen(ts) {
  if (!ts?.seconds) return '—';
  const diff = Math.max(0, Date.now() - Number(ts.seconds) * 1000);
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function stripTrailingDot(s) {
  return s && s.endsWith('.') ? s.slice(0, -1) : s || '';
}

export default function BLOSPage() {
  const [status, setStatus] = useState(null);
  const [peers, setPeers] = useState([]);
  const [events, setEvents] = useState([]);
  const [enableBlos, setEnableBlos] = useState(false);
  const [authKey, setAuthKey] = useState('');
  const [loginServer, setLoginServer] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [success, setSuccess] = useState(null);

  const fetchStatus = useCallback(async () => {
    try {
      const resp = await blosClient.getBLOSStatus({});
      setStatus(resp);
      setEnableBlos(resp.blosEnabled ?? false);
      setError(null);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchPeers = useCallback(async () => {
    try {
      const resp = await blosClient.listBLOSPeers({});
      setPeers(resp.peers ?? []);
    } catch {
      // non-fatal — keep previous peer list
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    fetchPeers();
    const id = setInterval(() => {
      fetchStatus();
      fetchPeers();
    }, 10_000);
    return () => clearInterval(id);
  }, [fetchStatus, fetchPeers]);

  // Event stream: open when BLOS is running, close on unmount or disable.
  const streamAbortRef = useRef(null);
  const running = status?.blosEnabled ?? false;

  useEffect(() => {
    if (!running) return undefined;

    const controller = new AbortController();
    streamAbortRef.current = controller;

    (async () => {
      try {
        const stream = blosClient.streamBLOSEvents({}, { signal: controller.signal });
        // Server wraps each BLOSEvent in a StreamBLOSEventsResponse envelope.
        // Unwrap to keep the events array holding bare BLOSEvent instances.
        for await (const resp of stream) {
          const ev = resp?.event;
          if (!ev) continue;
          setEvents((prev) => {
            const next = [...prev, ev];
            return next.length > MAX_EVENTS ? next.slice(-MAX_EVENTS) : next;
          });
        }
      } catch {
        // Stream aborted on unmount or network failure — swallow silently;
        // the next mount/re-enable will reopen it.
      }
    })();

    return () => {
      controller.abort();
      streamAbortRef.current = null;
    };
  }, [running]);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const req = { enableBlos, authKey };
      if (loginServer) req.loginServerUrl = loginServer;
      const resp = await blosClient.updateBLOSConfig(req);
      if (resp.success === false) throw new Error(resp.message || 'Update failed');
      setSuccess(resp.message || 'BLOS configuration updated.');
      setAuthKey('');
      fetchStatus();
    } catch (e) {
      setError('Failed to update BLOS: ' + e.message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="blos-loading">Loading BLOS status...</div>
    );
  }

  const enabled = status?.blosEnabled ?? false;
  const statusMsg = status?.message || '';
  const identity = status?.identity;
  const derp = status?.derp;
  const counters = status?.counters;
  const network = status?.network;

  const overlayIp = identity?.overlayIps?.[0] || '—';
  const hostname = stripTrailingDot(identity?.dnsName) || identity?.hostname || '—';
  const since = formatSince(identity?.connectedSince);

  return (
    <>
      <div className="lat-topbar">
        <div className="node-id">
          BLOS
          <span className="ip">{enabled ? 'TUNNEL UP' : 'TUNNEL DOWN'}</span>
        </div>
        <div className="chips">
          <span className={`lat-chip ${enabled ? 'ok' : 'crit'}`}>
            <span className="dot" />
            {enabled ? 'ENABLED' : 'DISABLED'}
          </span>
        </div>
      </div>
      <div className="lat-view-header">
        <div>
          <h2>◇ BLOS · Beyond Line of Sight</h2>
          <div className="crumb">Tailscale overlay</div>
        </div>
        <div className="lat-view-toolbar">
          <button className="lat-btn ghost" type="button" title="Rotate authentication key">ROTATE KEY</button>
        </div>
      </div>

      {error && <div className="blos-banner crit">{error}</div>}
      {success && <div className="blos-banner ok">{success}</div>}

      <div className="lat-body blos-grid">

        {/* ── Tunnel status card ───────────────────────────────── */}
        <div className="lat-panel">
          <div className="panel-head"><h3>Tunnel</h3></div>
          <div className={`big-num ${enabled ? 'ok' : 'crit'}`}>
            {enabled ? 'Enabled' : 'Disabled'}
          </div>
          {statusMsg && <div className="blos-sub">{statusMsg}</div>}
          <div className="kv"><span className="k">Overlay IP</span><span className="v">{overlayIp}</span></div>
          <div className="kv"><span className="k">Hostname</span><span className="v">{hostname}</span></div>
          <div className="kv"><span className="k">Since</span><span className="v">{since}</span></div>
        </div>

        {/* ── RX / TX / DERP KPI cards ─────────────────────────── */}
        <div className="lat-panel">
          <div className="panel-head"><h3>RX · 60s</h3></div>
          <div className="big-num">
            {formatKbps(counters?.rxBytesPerSec)}
            <span className="unit">kbps</span>
          </div>
          <div className="kv">
            <span className="k">Total RX</span>
            <span className="v">{formatBytes(counters?.rxBytesTotal)}</span>
          </div>
        </div>

        <div className="lat-panel">
          <div className="panel-head"><h3>TX · 60s</h3></div>
          <div className="big-num">
            {formatKbps(counters?.txBytesPerSec)}
            <span className="unit">kbps</span>
          </div>
          <div className="kv">
            <span className="k">Total TX</span>
            <span className="v">{formatBytes(counters?.txBytesTotal)}</span>
          </div>
        </div>

        <div className="lat-panel">
          <div className="panel-head"><h3>DERP Relay</h3></div>
          <div className="kv"><span className="k">Region</span><span className="v">{derp?.region || '—'}</span></div>
          <div className="kv">
            <span className="k">Latency</span>
            <span className="v">{derp?.latencyMs ? `${derp.latencyMs} ms` : '—'}</span>
          </div>
          <div className="kv">
            <span className="k">Path</span>
            <span className={`v${derp?.path === 'derp' ? ' warn' : ''}`}>{derp?.path || '—'}</span>
          </div>
          <div className="kv"><span className="k">Endpoint</span><span className="v mono-v">{derp?.endpoint || '—'}</span></div>
          <div className="kv">
            <span className="k">Keepalive</span>
            <span className="v">
              {derp?.keepaliveInterval?.seconds
                ? `${Number(derp.keepaliveInterval.seconds)}s`
                : '—'}
            </span>
          </div>
        </div>

        {/* ── Overlay peers + ACL tags ───────────────────────── */}
        <div className="lat-panel col-span-3">
          <div className="panel-head">
            <h3>Overlay Peers</h3>
            <span className="panel-head-right">{peers.length} visible</span>
          </div>
          {peers.length === 0 ? (
            <div className="blos-empty">
              {enabled ? 'No overlay peers yet.' : 'BLOS is disabled.'}
            </div>
          ) : (
            <div className="blos-peers">
              <div className="blos-peer-row head">
                <span>Host</span>
                <span>Overlay IP</span>
                <span>Endpoint</span>
                <span>Path</span>
                <span>RX</span>
                <span>TX</span>
                <span>Last seen</span>
              </div>
              {peers.map((p) => (
                <div className="blos-peer-row" key={p.dnsName || p.hostname}>
                  <span className="blos-peer-host">
                    <span className={`dot ${p.online ? 'ok' : 'crit'}`} />
                    {p.hostname || stripTrailingDot(p.dnsName) || '—'}
                  </span>
                  <span className="mono-v">{p.overlayIps?.[0] || '—'}</span>
                  <span className="mono-v">{p.endpoint || '—'}</span>
                  <span className={p.path === 'derp' ? 'warn' : ''}>{p.path || '—'}</span>
                  <span>{formatBytes(p.rxBytes)}</span>
                  <span>{formatBytes(p.txBytes)}</span>
                  <span>{formatLastSeen(p.lastSeen)}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="lat-panel">
          <div className="panel-head"><h3>ACL / Tags</h3></div>
          <div className="kv">
            <span className="k">Exit node</span>
            <span className="v">{network?.exitNode ? 'Yes' : 'No'}</span>
          </div>
          <div className="kv">
            <span className="k">Subnet routes</span>
            <span className="v">{network?.advertisedRoutes?.join(', ') || '—'}</span>
          </div>
          <div className="kv">
            <span className="k">Tags</span>
            <span className="v">{network?.aclTags?.join(', ') || '—'}</span>
          </div>
        </div>

        {/* ── Configuration ──────────────────────────────────── */}
        <div className="lat-panel col-span-2">
          <div className="panel-head"><h3>Configuration</h3></div>

          <div className="blos-toggle-row">
            <label>Enable BLOS</label>
            <button
              type="button"
              className={`lat-toggle${enableBlos ? ' on' : ''}`}
              onClick={() => setEnableBlos(!enableBlos)}
            >
              <span className="track"><span className="thumb" /></span>
              <span className="label">{enableBlos ? 'On' : 'Off'}</span>
            </button>
          </div>

          <div className="lat-field">
            <label htmlFor="blos-auth-key">Auth Key</label>
            <input
              id="blos-auth-key"
              className="lat-input"
              type="password"
              value={authKey}
              onChange={(e) => setAuthKey(e.target.value)}
              placeholder="Paste auth key"
              autoComplete="off"
            />
            <span className="hint">Never echoed back · blank keeps current</span>
          </div>

          <div className="lat-field">
            <label htmlFor="blos-login-server">Login Server URL (optional)</label>
            <input
              id="blos-login-server"
              className="lat-input"
              value={loginServer}
              onChange={(e) => setLoginServer(e.target.value)}
              placeholder="https://headscale.example.com"
              autoComplete="off"
            />
          </div>

          <div className="blos-actions">
            <button
              className="lat-btn primary"
              onClick={handleSave}
              disabled={saving}
              type="button"
            >
              {saving ? 'Saving…' : 'Update BLOS Config'}
            </button>
          </div>
        </div>

        <div className="lat-panel col-span-2">
          <div className="panel-head"><h3>Keepalive &amp; Events</h3></div>
          {events.length === 0 ? (
            <div className="blos-empty">
              {enabled ? 'Waiting for events…' : 'BLOS is disabled.'}
            </div>
          ) : (
            <div className="blos-events">
              {events.slice().reverse().map((ev, i) => (
                <div className="blos-event-row" key={`${ev.ts?.seconds}-${ev.ts?.nanos}-${i}`}>
                  <span className="blos-event-kind">
                    {EVENT_KIND_LABELS[ev.kind] || 'EVT'}
                  </span>
                  <span className="blos-event-subject">{ev.subject || '—'}</span>
                  <span className="blos-event-msg">{ev.message}</span>
                </div>
              ))}
            </div>
          )}
        </div>

      </div>
    </>
  );
}
