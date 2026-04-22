// =============================================================================
// BLOS.jsx — Beyond Line of Sight (VPN/Tailscale) configuration page
// =============================================================================
// The reimagined Lattice layout includes panels for traffic, DERP relay,
// overlay peers, ACL tags, and a keepalive log.  Many of those fields are
// NOT currently exposed by the BLOS proto (see the "Data Provenance" section
// of the Lattice redesign plan) — those panels render em-dashes with TODO
// comments until a follow-up plan extends the API.

import { useState, useEffect, useCallback } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { BLOSService } from "../gen/openmanet/blos/v1/blos_service_connect.js";
import './BLOS.css';

const blosClient = createClient(BLOSService, transport);

export default function BLOSPage() {
  const [status, setStatus] = useState(null);
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

  useEffect(() => { fetchStatus(); }, [fetchStatus]);

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
          {/* TODO(api-plan): surface overlay IPv4/IPv6, hostname, connected_since.
              Currently the BLOS proto exposes only blos_enabled + message. */}
          <div className="kv"><span className="k">Overlay IP</span><span className="v">—</span></div>
          <div className="kv"><span className="k">Hostname</span><span className="v">—</span></div>
          <div className="kv"><span className="k">Since</span><span className="v">—</span></div>
        </div>

        {/* ── RX / TX / DERP KPI cards — placeholders until API gains them ── */}
        <div className="lat-panel">
          <div className="panel-head"><h3>RX · 60s</h3></div>
          {/* TODO(api-plan): rx_bytes_total + rate history */}
          <div className="big-num">—<span className="unit">kbps</span></div>
          <div className="kv"><span className="k">Total RX</span><span className="v">—</span></div>
        </div>

        <div className="lat-panel">
          <div className="panel-head"><h3>TX · 60s</h3></div>
          {/* TODO(api-plan): tx_bytes_total + rate history */}
          <div className="big-num">—<span className="unit">kbps</span></div>
          <div className="kv"><span className="k">Total TX</span><span className="v">—</span></div>
        </div>

        <div className="lat-panel">
          <div className="panel-head"><h3>DERP Relay</h3></div>
          {/* TODO(api-plan): derp_region, derp_latency_ms, endpoint, keepalive */}
          <div className="kv"><span className="k">Region</span><span className="v">—</span></div>
          <div className="kv"><span className="k">Latency</span><span className="v">—</span></div>
          <div className="kv"><span className="k">Path</span><span className="v">—</span></div>
          <div className="kv"><span className="k">Endpoint</span><span className="v">—</span></div>
          <div className="kv"><span className="k">Keepalive</span><span className="v">—</span></div>
        </div>

        {/* ── Overlay peers + ACL tags ───────────────────────── */}
        <div className="lat-panel col-span-3">
          <div className="panel-head">
            <h3>Overlay Peers</h3>
          </div>
          {/* TODO(api-plan): per-peer hostname, overlay_ip, os, endpoint, latency, rx, tx, path, last_seen */}
          <div className="blos-empty">No overlay-peer data exposed by current BLOS proto.</div>
        </div>

        <div className="lat-panel">
          <div className="panel-head"><h3>ACL / Tags</h3></div>
          {/* TODO(api-plan): acl_tags[], exit_node, advertised_routes[] */}
          <div className="kv"><span className="k">Exit node</span><span className="v">—</span></div>
          <div className="kv"><span className="k">Subnet routes</span><span className="v">—</span></div>
          <div className="kv"><span className="k">Tags</span><span className="v">—</span></div>
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
              {saving ? 'Saving\u2026' : 'Update BLOS Config'}
            </button>
          </div>
        </div>

        <div className="lat-panel col-span-2">
          <div className="panel-head"><h3>Keepalive &amp; Events</h3></div>
          {/* TODO(api-plan): expose keepalive events via a BLOS events stream */}
          <div className="blos-empty">No event stream exposed by current BLOS proto.</div>
        </div>

      </div>
    </>
  );
}
