// =============================================================================
// GpsStatus.jsx — GPS / GNSS status and configuration page
// =============================================================================

import { useState, useEffect, useCallback, useRef } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { GNSSService } from "../gen/openmanet/gnss/v1/gnss_service_connect.js";

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

// ── Map View (placeholder) ──────────────────────────────────────────────────
function MapView({ position }) {
  const lat = position?.latitude;
  const lon = position?.longitude;
  const alt = position?.altitude;
  const hasPos = lat != null && lon != null && (lat !== 0 || lon !== 0);

  return (
    <div className="card">
      <div className="card-title">Map View</div>
      <div style={{
        display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
        minHeight: 180, color: 'var(--muted)', fontSize: '0.85em',
      }}>
        <div style={{ fontSize: '2em', marginBottom: 8, opacity: 0.4 }}>{'\uD83C\uDF0D'}</div>
        <div style={{ marginBottom: 4 }}>Map View</div>
        {hasPos ? (
          <>
            <div style={{ fontFamily: 'monospace', color: 'var(--green)', fontSize: '0.95em' }}>
              {lat.toFixed(4)} {lat >= 0 ? 'N' : 'S'}, {Math.abs(lon).toFixed(4)} {lon >= 0 ? 'E' : 'W'}
            </div>
            {alt != null && <div style={{ fontSize: '0.85em', marginTop: 2 }}>Altitude: {alt.toFixed(0)}m MSL</div>}
          </>
        ) : (
          <div>No position data</div>
        )}
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

  const handleSave = async () => {
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
  };

  if (loading) {
    return (
      <div style={{ width: '100%', maxWidth: 1100 }}>
        <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>GPS / GNSS</h2>
        <div className="card" style={{ textAlign: 'center', padding: 40 }}>
          <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>Loading GNSS data...</div>
        </div>
      </div>
    );
  }

  const position = status?.position;
  const satelliteStatus = status?.satelliteStatus;

  return (
    <div style={{ width: '100%', maxWidth: 1100 }}>
      <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>GPS / GNSS</h2>

      {error && (
        <div style={{
          background: 'rgba(204,51,51,0.1)', border: '1px solid var(--red)', borderRadius: 6,
          padding: '8px 12px', marginBottom: 8, fontSize: '0.85em', color: 'var(--red)',
        }}>{error}</div>
      )}
      {success && (
        <div style={{
          background: 'rgba(107,142,35,0.1)', border: '1px solid var(--green)', borderRadius: 6,
          padding: '8px 12px', marginBottom: 8, fontSize: '0.85em', color: 'var(--green)',
        }}>{success}</div>
      )}

      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))',
        gap: 8,
      }}>
        <MapView position={position} />
        <PositionPanel position={position} satelliteStatus={satelliteStatus} />
        <SatellitePanel satelliteStatus={satelliteStatus} />
        <SettingsPanel config={config} onConfigChange={setConfig} onSave={handleSave} saving={saving} />
      </div>
    </div>
  );
}
