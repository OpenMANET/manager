// =============================================================================
// GpsStatus.jsx — GPS / GNSS status and configuration page
// =============================================================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { GNSSService } from "../gen/openmanet/gnss/v1/gnss_service_connect.js";
import WidgetGrid from '../components/WidgetGrid.jsx';
import SkyPlot from '../components/SkyPlot.jsx';
import './GpsStatus.css';

const gnssClient = createClient(GNSSService, transport);

const FIX_LABELS = { 0: 'No Fix', 1: 'No Fix', 2: '2D Fix', 3: '3D Fix' };
const POLL_INTERVAL = 2000;

function snrColor(snr) {
  if (snr >= 25) return 'var(--green)';
  if (snr >= 15) return 'var(--yellow)';
  return 'var(--red)';
}

function formatTime(ts) {
  if (!ts) return '-';
  const d = ts instanceof Date ? ts : new Date(ts);
  if (isNaN(d.getTime())) return '-';
  return d.toISOString().slice(11, 19) + ' UTC';
}

// ── Globe View ──────────────────────────────────────────────────────────────
import { coastlines } from '../data/coastlines.js';

const GLOBE_SIZE = 420;
const MIN_ZOOM = 0.8;
const MAX_ZOOM = 4;
const DEG2RAD = Math.PI / 180;

function drawGlobe(canvas, viewLat, viewLon, zoom) {
  const ctx = canvas.getContext('2d');
  if (!ctx) return;

  const dpr = window.devicePixelRatio || 1;
  canvas.width = GLOBE_SIZE * dpr;
  canvas.height = GLOBE_SIZE * dpr;
  canvas.style.width = GLOBE_SIZE + 'px';
  canvas.style.height = GLOBE_SIZE + 'px';
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, GLOBE_SIZE, GLOBE_SIZE);

  const cx = GLOBE_SIZE / 2;
  const cy = GLOBE_SIZE / 2;
  const r = (GLOBE_SIZE / 2 - 16) * zoom;

  const lonRad = -viewLon * DEG2RAD;
  const latRad = viewLat * DEG2RAD;
  const cosLatR = Math.cos(latRad);
  const sinLatR = Math.sin(latRad);

  // Project lat/lon to screen coordinates. Returns { x, y, z } always.
  // z > 0 means the point is on the visible hemisphere.
  function projectFull(pLat, pLon) {
    const phi = pLat * DEG2RAD;
    const lam = pLon * DEG2RAD + lonRad;
    const cosPhi = Math.cos(phi);
    const x3 = cosPhi * Math.sin(lam);
    const y3 = Math.sin(phi);
    const z3 = cosPhi * Math.cos(lam);
    const yr = y3 * cosLatR - z3 * sinLatR;
    const zr = y3 * sinLatR + z3 * cosLatR;
    return { x: cx + x3 * r, y: cy - yr * r, z: zr };
  }

  // Convenience: returns null for back-face (used by grid lines).
  function project(pLat, pLon) {
    const p = projectFull(pLat, pLon);
    return p.z < -0.05 ? null : p;
  }

  // Clip a polygon to the visible hemisphere. Where edges cross the
  // horizon, insert points along the globe rim arc so the fill follows
  // the circular edge instead of cutting straight across.
  function clipToHemisphere(poly) {
    const full = poly.map((pt) => projectFull(pt[1], pt[0]));
    const clipped = [];
    const threshold = 0;

    // Interpolate the horizon crossing and snap it onto the globe rim.
    function edgePoint(a, b) {
      const t = (threshold - a.z) / (b.z - a.z);
      const ix = a.x + t * (b.x - a.x);
      const iy = a.y + t * (b.y - a.y);
      // Snap to rim: normalize direction from center, scale to radius.
      const dx = ix - cx, dy = iy - cy;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      return { x: cx + (dx / dist) * r, y: cy + (dy / dist) * r };
    }

    // Insert arc points along the globe rim between two rim points.
    function addRimArc(from, to) {
      const a1 = Math.atan2(from.y - cy, from.x - cx);
      const a2 = Math.atan2(to.y - cy, to.x - cx);
      let delta = a2 - a1;
      // Pick the shorter arc direction.
      if (delta > Math.PI) delta -= 2 * Math.PI;
      if (delta < -Math.PI) delta += 2 * Math.PI;
      const steps = Math.max(2, Math.ceil(Math.abs(delta) / 0.15));
      for (let s = 1; s < steps; s++) {
        const a = a1 + (delta * s) / steps;
        clipped.push({ x: cx + Math.cos(a) * r, y: cy + Math.sin(a) * r });
      }
    }

    let lastExit = null;

    for (let i = 0; i < full.length; i++) {
      const curr = full[i];
      const prev = full[(i + full.length - 1) % full.length];
      const currVisible = curr.z >= threshold;
      const prevVisible = prev.z >= threshold;

      if (prevVisible && !currVisible) {
        // Exiting visible hemisphere.
        const ep = edgePoint(prev, curr);
        clipped.push(ep);
        lastExit = ep;
      } else if (!prevVisible && currVisible) {
        // Entering visible hemisphere.
        const ep = edgePoint(prev, curr);
        // Walk along the rim from last exit to this entry.
        if (lastExit) {
          addRimArc(lastExit, ep);
        }
        clipped.push(ep);
      }

      if (currVisible) {
        clipped.push({ x: curr.x, y: curr.y });
      }
    }

    return clipped;
  }

  // Clip to globe circle.
  ctx.save();
  ctx.beginPath();
  ctx.arc(cx, cy, (GLOBE_SIZE / 2 - 16) * Math.max(zoom, 1), 0, Math.PI * 2);
  ctx.clip();

  // Globe background — dark cool blue, Lattice accent edge.
  ctx.beginPath();
  ctx.arc(cx, cy, r, 0, Math.PI * 2);
  ctx.fillStyle = '#0b161e';
  ctx.fill();

  // Grid lines — cyan, brighter on equator + prime meridian.
  ctx.lineWidth = 0.4;
  for (let gLat = -80; gLat <= 80; gLat += 20) {
    ctx.beginPath();
    let started = false;
    for (let gLon = -180; gLon <= 180; gLon += 3) {
      const p = project(gLat, gLon);
      if (!p) { started = false; continue; }
      if (!started) { ctx.moveTo(p.x, p.y); started = true; }
      else ctx.lineTo(p.x, p.y);
    }
    ctx.strokeStyle = gLat === 0 ? 'rgba(0,229,255,0.3)' : 'rgba(0,229,255,0.14)';
    ctx.stroke();
  }
  for (let gLon = -180; gLon < 180; gLon += 30) {
    ctx.beginPath();
    let started = false;
    for (let gLat = -90; gLat <= 90; gLat += 3) {
      const p = project(gLat, gLon);
      if (!p) { started = false; continue; }
      if (!started) { ctx.moveTo(p.x, p.y); started = true; }
      else ctx.lineTo(p.x, p.y);
    }
    ctx.strokeStyle = gLon === 0 ? 'rgba(0,229,255,0.3)' : 'rgba(0,229,255,0.14)';
    ctx.stroke();
  }

  // Draw coastlines — Lattice ok-green fill + stroke.
  coastlines.forEach((poly) => {
    const clipped = clipToHemisphere(poly);
    if (clipped.length < 3) return;

    ctx.beginPath();
    ctx.moveTo(clipped[0].x, clipped[0].y);
    for (let i = 1; i < clipped.length; i++) {
      ctx.lineTo(clipped[i].x, clipped[i].y);
    }
    ctx.closePath();
    ctx.fillStyle = 'rgba(0,230,118,0.18)';
    ctx.fill();

    ctx.lineWidth = 1.2;
    ctx.strokeStyle = 'rgba(0,230,118,0.5)';
    ctx.beginPath();
    let started = false;
    for (let i = 0; i < poly.length; i++) {
      const p = project(poly[i][1], poly[i][0]);
      if (!p) { started = false; continue; }
      if (!started) { ctx.moveTo(p.x, p.y); started = true; }
      else ctx.lineTo(p.x, p.y);
    }
    ctx.stroke();
  });

  // Globe rim — cyan edge at full radius.
  ctx.restore();
  ctx.beginPath();
  ctx.arc(cx, cy, (GLOBE_SIZE / 2 - 16) * Math.max(zoom, 1), 0, Math.PI * 2);
  ctx.strokeStyle = 'rgba(0,229,255,0.45)';
  ctx.lineWidth = 1.2;
  ctx.stroke();

  return project;
}

function MapView({ position }) {
  const canvasRef = useRef(null);
  const viewRef = useRef({ lat: 20, lon: 0, zoom: 1 });
  const dragRef = useRef(null);
  const [, forceRender] = useState(0);

  const lat = position?.latitude;
  const lon = position?.longitude;
  const alt = position?.altitude;
  const hasPos = lat != null && lon != null && (lat !== 0 || lon !== 0);

  // Center on position when it first becomes available.
  const centeredRef = useRef(false);
  if (hasPos && !centeredRef.current) {
    viewRef.current.lat = lat;
    viewRef.current.lon = lon;
    centeredRef.current = true;
  }

  // Draw the globe.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const v = viewRef.current;
    const project = drawGlobe(canvas, v.lat, v.lon, v.zoom);
    if (!project) return;

    // Plot position marker — cyan pulse.
    if (hasPos) {
      const ctx = canvas.getContext('2d');
      const p = project(lat, lon);
      if (p && ctx) {
        ctx.beginPath();
        ctx.arc(p.x, p.y, 14, 0, Math.PI * 2);
        ctx.fillStyle = 'rgba(0,229,255,0.12)';
        ctx.fill();

        ctx.beginPath();
        ctx.arc(p.x, p.y, 8, 0, Math.PI * 2);
        ctx.strokeStyle = 'rgba(0,229,255,0.6)';
        ctx.lineWidth = 2;
        ctx.stroke();

        ctx.beginPath();
        ctx.arc(p.x, p.y, 4, 0, Math.PI * 2);
        ctx.fillStyle = '#00e5ff';
        ctx.fill();
      }
    }
  });

  // Mouse/touch handlers for drag-to-rotate and scroll-to-zoom.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const onWheel = (e) => {
      e.preventDefault();
      const v = viewRef.current;
      const delta = e.deltaY > 0 ? 0.9 : 1.1;
      v.zoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, v.zoom * delta));
      forceRender((n) => n + 1);
    };

    const onPointerDown = (e) => {
      canvas.setPointerCapture(e.pointerId);
      dragRef.current = { x: e.clientX, y: e.clientY };
    };

    const onPointerMove = (e) => {
      if (!dragRef.current) return;
      const dx = e.clientX - dragRef.current.x;
      const dy = e.clientY - dragRef.current.y;
      dragRef.current = { x: e.clientX, y: e.clientY };
      const v = viewRef.current;
      const sensitivity = 0.3 / v.zoom;
      v.lon -= dx * sensitivity;
      v.lat += dy * sensitivity;
      v.lat = Math.max(-85, Math.min(85, v.lat));
      forceRender((n) => n + 1);
    };

    const onPointerUp = () => {
      dragRef.current = null;
    };

    canvas.addEventListener('wheel', onWheel, { passive: false });
    canvas.addEventListener('pointerdown', onPointerDown);
    canvas.addEventListener('pointermove', onPointerMove);
    canvas.addEventListener('pointerup', onPointerUp);
    canvas.addEventListener('pointercancel', onPointerUp);

    return () => {
      canvas.removeEventListener('wheel', onWheel);
      canvas.removeEventListener('pointerdown', onPointerDown);
      canvas.removeEventListener('pointermove', onPointerMove);
      canvas.removeEventListener('pointerup', onPointerUp);
      canvas.removeEventListener('pointercancel', onPointerUp);
    };
  }, []);

  const resetView = () => {
    const v = viewRef.current;
    if (hasPos) {
      v.lat = lat;
      v.lon = lon;
    } else {
      v.lat = 20;
      v.lon = 0;
    }
    v.zoom = 1;
    forceRender((n) => n + 1);
  };

  return (
    <div className="card">
      <div className="card-title" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        Globe View
        <button
          onClick={resetView}
          style={{
            background: 'var(--border)', border: 'none', borderRadius: 4,
            color: 'var(--muted)', fontSize: '0.75em', padding: '3px 8px', cursor: 'pointer',
          }}
        >
          Reset
        </button>
      </div>
      <div style={{
        display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
        padding: '12px 0',
      }}>
        <canvas
          ref={canvasRef}
          style={{ display: 'block', marginBottom: 10, cursor: 'grab', touchAction: 'none' }}
        />
        {hasPos ? (
          <div style={{ textAlign: 'center', fontSize: '0.85em' }}>
            <div style={{ fontFamily: 'monospace', color: 'var(--green)', fontSize: '1em', marginBottom: 2 }}>
              {lat.toFixed(6)} {lat >= 0 ? 'N' : 'S'}, {Math.abs(lon).toFixed(6)} {lon >= 0 ? 'E' : 'W'}
            </div>
            {alt != null && <div style={{ color: 'var(--muted)' }}>Altitude: {alt.toFixed(0)}m MSL</div>}
          </div>
        ) : (
          <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>No position data</div>
        )}
        <div style={{ color: 'var(--muted)', fontSize: '0.72em', marginTop: 4 }}>
          Drag to rotate, scroll to zoom
        </div>
      </div>
    </div>
  );
}

// ── Sky Plot Panel ──────────────────────────────────────────────────────────
function SkyPlotPanel({ satelliteStatus }) {
  const sats = satelliteStatus?.satellites ?? [];
  const used = satelliteStatus?.satellitesUsed ?? 0;
  const inView = satelliteStatus?.satellitesInView ?? sats.length;

  return (
    <div className="card">
      <div className="card-title">Sky Plot ({used} used / {inView} in view)</div>
      <div style={{
        display: 'flex', flexDirection: 'column', alignItems: 'center',
        padding: '4px 0',
      }}>
        <SkyPlot satellites={sats} />
        <div style={{
          color: 'var(--muted)', fontFamily: 'var(--font-mono)',
          fontSize: '0.72em', marginTop: 6,
          letterSpacing: '0.14em', textTransform: 'uppercase',
        }}>
          Zenith · N up · rings = 30° elevation
        </div>
      </div>
    </div>
  );
}

// ── Position Panel ──────────────────────────────────────────────────────────
function PositionPanel({ position }) {
  const fixType = position?.fixType ?? 0;
  const hasfix = fixType >= 2;

  const rows = [
    { label: 'Fix Type', value: (
      <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <span className="status-dot" style={{ background: hasfix ? 'var(--green)' : 'var(--red)' }} />
        <span style={{ fontWeight: 600 }}>{FIX_LABELS[fixType] ?? 'Unknown'}</span>
      </span>
    )},
    { label: 'Latitude', value: position?.latitude != null ? position.latitude.toFixed(6) : '-' },
    { label: 'Longitude', value: position?.longitude != null ? position.longitude.toFixed(6) : '-' },
    { label: 'Altitude', value: position?.altitude != null ? `${position.altitude.toFixed(0)} m MSL` : '-' },
    { label: 'Speed', value: position?.speed != null ? `${(position.speed * 3.6).toFixed(1)} km/h` : '-' },
    { label: 'Heading', value: position?.heading != null ? `${position.heading.toFixed(1)} deg` : '-' },
    { label: 'PDOP', value: position?.pdop != null && position.pdop > 0 ? position.pdop.toFixed(1) : '-' },
    { label: 'HDOP', value: position?.hdop != null && position.hdop > 0 ? position.hdop.toFixed(1) : '-' },
    { label: 'Last Update', value: formatTime(position?.lastUpdate) },
  ];

  return (
    <div className="card">
      <div className="card-title">Position</div>
      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85em' }}>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
              <td style={{ padding: '5px 8px', color: 'var(--muted)', whiteSpace: 'nowrap' }}>{r.label}</td>
              <td style={{ padding: '5px 8px', textAlign: 'right', fontWeight: 600, fontFamily: typeof r.value === 'string' ? 'monospace' : undefined }}>
                {r.value}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ── Satellite Table ─────────────────────────────────────────────────────────
function SatellitePanel({ satelliteStatus }) {
  const sats = satelliteStatus?.satellites ?? [];
  const used = satelliteStatus?.satellitesUsed ?? 0;
  const inView = satelliteStatus?.satellitesInView ?? sats.length;

  return (
    <div className="card">
      <div className="card-title">
        Satellites ({used} used / {inView} in view)
      </div>
      {sats.length === 0 ? (
        <div style={{ color: 'var(--muted)', fontSize: '0.85em', padding: '12px 0' }}>No satellite data available.</div>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.82em' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)', color: 'var(--muted)' }}>
                <th style={{ textAlign: 'left', padding: '4px 8px' }}>PRN</th>
                <th style={{ textAlign: 'right', padding: '4px 8px' }}>ELEV</th>
                <th style={{ textAlign: 'right', padding: '4px 8px' }}>AZIM</th>
                <th style={{ textAlign: 'left', padding: '4px 8px', width: '30%' }}>SNR</th>
                <th style={{ textAlign: 'center', padding: '4px 8px' }}>USED</th>
              </tr>
            </thead>
            <tbody>
              {sats.map((sat, i) => (
                <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                  <td style={{ padding: '5px 8px', fontWeight: 600 }}>G{sat.prn}</td>
                  <td style={{ padding: '5px 8px', textAlign: 'right' }}>{sat.elevation?.toFixed(0) ?? '-'} deg</td>
                  <td style={{ padding: '5px 8px', textAlign: 'right' }}>{sat.azimuth?.toFixed(0) ?? '-'} deg</td>
                  <td style={{ padding: '5px 8px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <div style={{
                        flex: 1, height: 10, background: 'var(--border)', borderRadius: 3, overflow: 'hidden',
                      }}>
                        <div style={{
                          width: `${Math.min((sat.snr / 50) * 100, 100)}%`,
                          height: '100%', borderRadius: 3,
                          background: snrColor(sat.snr),
                          transition: 'width 0.3s',
                        }} />
                      </div>
                      <span style={{ minWidth: 24, textAlign: 'right', fontSize: '0.9em', fontFamily: 'monospace' }}>
                        {sat.snr?.toFixed(0) ?? '-'}
                      </span>
                    </div>
                  </td>
                  <td style={{ textAlign: 'center', padding: '5px 8px' }}>
                    <span className="status-dot" style={{
                      background: sat.used ? 'var(--green)' : 'var(--border)',
                    }} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ── GPS Settings & Output Protocols ─────────────────────────────────────────
function SettingsPanel({ config, onConfigChange, onSave, saving }) {
  const settings = config?.settings ?? {};
  const output = config?.outputProtocols ?? {};

  const update = (section, key, val) => {
    onConfigChange({ ...config, [section]: { ...config[section], [key]: val } });
  };

  const inputStyle = {
    background: 'var(--bg)', color: 'var(--text)', border: '1px solid var(--border)',
    borderRadius: 6, padding: '6px 10px', fontSize: '0.88em', outline: 'none', width: '100%', maxWidth: 350,
  };
  const btnStyle = {
    padding: '8px 20px', border: 'none', borderRadius: 6, cursor: 'pointer',
    fontSize: '0.85em', fontWeight: 600, background: 'var(--accent)', color: 'var(--text)',
  };

  return (
    <div className="card">
      <div className="card-title">GPS Settings</div>
      <div style={{ marginBottom: 16 }}>
        <Toggle
          label="Enable GPS"
          checked={settings.enableGps ?? false}
          onChange={(v) => update('settings', 'enableGps', v)}
        />
      </div>

      <div className="card-title" style={{ marginTop: 12 }}>Output Protocols</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Toggle
          label="Send as NMEA"
          checked={output.sendAsNmea ?? false}
          onChange={(v) => update('outputProtocols', 'sendAsNmea', v)}
        />
        <Toggle
          label="Send as CoT (Cursor on Target)"
          checked={output.sendAsCot ?? false}
          onChange={(v) => update('outputProtocols', 'sendAsCot', v)}
        />
        <div>
          <label style={{ fontSize: '0.82em', color: 'var(--muted)', display: 'block', marginBottom: 4 }}>CoT UID</label>
          <input
            style={inputStyle}
            value={output.cotUid ?? ''}
            onChange={(e) => update('outputProtocols', 'cotUid', e.target.value)}
            placeholder="Leave empty for hostname"
          />
        </div>
      </div>

      <div style={{ marginTop: 16 }}>
        <button onClick={onSave} disabled={saving} style={{ ...btnStyle, opacity: saving ? 0.5 : 1 }}>
          {saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>
    </div>
  );
}

// ── Toggle Switch ───────────────────────────────────────────────────────────
function Toggle({ label, checked, onChange }) {
  return (
    <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: '0.88em' }}>
      <div
        onClick={() => onChange(!checked)}
        style={{
          width: 42, height: 22, borderRadius: 11, padding: 2,
          background: checked ? 'var(--green)' : 'var(--border)',
          cursor: 'pointer', transition: 'background 0.2s', display: 'flex', alignItems: 'center',
        }}
      >
        <div style={{
          width: 18, height: 18, borderRadius: '50%', background: '#fff',
          transition: 'transform 0.2s',
          transform: checked ? 'translateX(20px)' : 'translateX(0)',
        }} />
      </div>
      {label}
    </label>
  );
}

// ── Widget configuration ───────────────────────────────────────────────────

const GPS_WIDGETS = [
  { id: 'globe',      label: 'Globe View',   minWidth: 25, defaultWidth: 33 },
  { id: 'skyplot',    label: 'Sky Plot',     minWidth: 25, defaultWidth: 33 },
  { id: 'position',   label: 'Position',     minWidth: 25, defaultWidth: 34 },
  { id: 'satellites', label: 'Satellites',   minWidth: 40, defaultWidth: 66 },
  { id: 'settings',   label: 'GPS Settings', minWidth: 40, defaultWidth: 34 },
];

// ── Main Page ───────────────────────────────────────────────────────────────
export default function GpsStatusPage() {
  const [status, setStatus] = useState(null);
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [success, setSuccess] = useState(null);
  const pollRef = useRef(null);

  const fetchStatus = useCallback(async () => {
    try {
      const resp = await gnssClient.getGNSSStatus({});
      setStatus(resp);
    } catch {
      // Status polling errors are non-fatal; just keep previous data.
    }
  }, []);

  const fetchConfig = useCallback(async () => {
    try {
      const resp = await gnssClient.getGNSSConfig({});
      setConfig(resp);
      setError(null);
    } catch (e) {
      setError('Failed to load GNSS config: ' + e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchConfig();
    fetchStatus();
  }, [fetchConfig, fetchStatus]);

  // Poll for status updates.
  useEffect(() => {
    pollRef.current = setInterval(fetchStatus, POLL_INTERVAL);
    return () => clearInterval(pollRef.current);
  }, [fetchStatus]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const resp = await gnssClient.updateGNSSConfig({
        settings: config.settings,
        outputProtocols: config.outputProtocols,
      });
      if (!resp.success) throw new Error(resp.message || 'Update failed');
      setSuccess(resp.message || 'GNSS configuration updated.');
      fetchConfig();
    } catch (e) {
      setError('Failed to save GNSS config: ' + e.message);
    } finally {
      setSaving(false);
    }
  }, [config, fetchConfig]);

  const position = status?.position;
  const satelliteStatus = status?.satelliteStatus;

  const renderWidget = useCallback((id) => {
    switch (id) {
      case 'globe':      return <MapView position={position} />;
      case 'skyplot':    return <SkyPlotPanel satelliteStatus={satelliteStatus} />;
      case 'position':   return <PositionPanel position={position} satelliteStatus={satelliteStatus} />;
      case 'satellites':  return <SatellitePanel satelliteStatus={satelliteStatus} />;
      case 'settings':   return <SettingsPanel config={config} onConfigChange={setConfig} onSave={handleSave} saving={saving} />;
      default:           return null;
    }
  }, [position, satelliteStatus, config, handleSave, saving]);

  if (loading) {
    return (
      <div style={{ width: '100%', maxWidth: '100%' }}>
        <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>GPS / GNSS</h2>
        <div className="card" style={{ textAlign: 'center', padding: 40 }}>
          <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>Loading GNSS data...</div>
        </div>
      </div>
    );
  }

  return (
    <>
      {error && (
        <div style={{
          background: 'rgba(204,51,51,0.1)', border: '1px solid var(--red)', borderRadius: 6,
          padding: '8px 12px', marginBottom: 8, fontSize: '0.85em', color: 'var(--red)', maxWidth: '100%',
        }}>{error}</div>
      )}
      {success && (
        <div style={{
          background: 'rgba(107,142,35,0.1)', border: '1px solid var(--green)', borderRadius: 6,
          padding: '8px 12px', marginBottom: 8, fontSize: '0.85em', color: 'var(--green)', maxWidth: '100%',
        }}>{success}</div>
      )}
      <WidgetGrid
        pageId="gps"
        title="GPS / GNSS"
        widgets={GPS_WIDGETS}
        renderWidget={renderWidget}
      />
    </>
  );
}
