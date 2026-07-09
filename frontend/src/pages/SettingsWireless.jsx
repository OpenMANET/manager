// =============================================================================
// SettingsWireless.jsx — Wireless radio configuration tab
// =============================================================================

import { useState, useEffect, useCallback } from 'react';
import { createClient } from '@connectrpc/connect';
import { transport } from '../services/connectClient.js';
import { WifiConfigService } from '../gen/openmanet/wifi_config/v1/wifi_config_service_pb.js';
import { WifiMode } from '../gen/openmanet/wifi_config/v1/wifi_config_pb.js';
import { POWER_LEVELS, dBmToLevel, levelToDbm } from './SettingsWireless.power.js';
import LatSelect from '../components/LatSelect.jsx';
import './SettingsWireless.css';

const wifiClient = createClient(WifiConfigService, transport);

// Band numeric values (mirrors the WifiBand enum). The generated WifiBand enum
// does not expose short-name aliases (only WIFI_BAND_*), so we use literals.
// Band 4 == S1G (Sub-1 GHz / HaLow). S1G radios always operate in mesh mode
// and do not expose a Mode selector.
const BAND_S1G = 4;
const BAND_LABELS = { 0: '?', 1: '2.4 GHz', 2: '5 GHz', 3: '6 GHz', 4: 'Sub-1 GHz', 5: '60 GHz' };

const MODE_OPTIONS = [
  { value: WifiMode.AP,      label: 'Access Point' },
  { value: WifiMode.MESH,    label: 'Mesh' },
  { value: WifiMode.STA,     label: 'Station' },
  { value: WifiMode.ADHOC,   label: 'Ad-Hoc' },
  { value: WifiMode.MONITOR, label: 'Monitor' },
];

const ENCRYPTION_LABELS = {
  0: 'Unspecified', 1: 'WPA3-SAE', 2: 'WPA2-PSK', 3: 'WPA-PSK',
  4: 'PSK Mixed', 5: 'None', 6: 'OWE',
};

const HT_MODE_LABELS = {
  0: '—', 1: 'No HT', 2: 'HT20', 3: 'HT40-', 4: 'HT40+', 5: 'HT40',
  6: 'VHT20', 7: 'VHT40', 8: 'VHT80', 9: 'VHT160',
  10: 'HE20', 11: 'HE40', 12: 'HE80', 13: 'HE160',
  14: 'S1G 1 MHz', 15: 'S1G 2 MHz', 16: 'S1G 4 MHz', 17: 'S1G 8 MHz',
};

function isMeshMode(mode) {
  return mode === WifiMode.MESH;
}

// seedTxPowerForDisplay picks an initial dBm to show in the selector when the
// stored UCI setting is unset (txPower == 0). Without this, dBmToLevel(0)
// always snaps to "Low" because 10 dBm is the nearest canonical value, which
// misleads operators into thinking the radio is running at low power. We
// prefer the live iwinfo reading (snapped to the nearest canonical level),
// and fall back to the medium canonical (17 dBm) when no status is available.
function seedTxPowerForDisplay(settings, status) {
  if (!settings) return settings;
  const stored = settings.txPower;
  if (stored != null && stored > 0) return settings;

  const live = status?.txPower;
  if (live != null && live > 0) {
    return { ...settings, txPower: levelToDbm(dBmToLevel(live)) };
  }
  return { ...settings, txPower: levelToDbm('medium') };
}

function settingsEqual(a, b) {
  if (!a || !b) return a === b;
  return (
    a.ssid === b.ssid &&
    (a.meshId ?? '') === (b.meshId ?? '') &&
    (a.password ?? '') === (b.password ?? '') &&
    a.channel === b.channel &&
    a.bandwidth === b.bandwidth &&
    a.txPower === b.txPower &&
    (a.country ?? '') === (b.country ?? '') &&
    a.encryption === b.encryption &&
    (a.disabled ?? false) === (b.disabled ?? false) &&
    (a.mode ?? 0) === (b.mode ?? 0)
  );
}

function PowerSelector({ valueDbm, onChange, hint }) {
  const selected = dBmToLevel(valueDbm);
  return (
    <div className="lat-field full">
      <label>Tx Power</label>
      <div className="power-selector" role="radiogroup" aria-label="TX power level">
        {POWER_LEVELS.map(p => (
          <button
            key={p.level}
            type="button"
            role="radio"
            aria-checked={selected === p.level}
            className={selected === p.level ? 'active' : ''}
            onClick={() => onChange(p.dBm)}
          >
            <span className="level">{p.label}</span>
            <span className="dbm">{p.dBm} dBm</span>
          </button>
        ))}
      </div>
      <div className="power-hint">
        <span>Selected: {POWER_LEVELS.find(p => p.level === selected)?.label} · {levelToDbm(selected)} dBm</span>
        {hint && <span className="readout">{hint}</span>}
      </div>
    </div>
  );
}

function ConnectedTable({ rows, kind }) {
  if (!rows || rows.length === 0) {
    return <div className="empty-row">No {kind === 'clients' ? 'connected clients' : 'mesh peers'}.</div>;
  }
  return (
    <table className="lat-table">
      <thead>
        <tr>
          <th>{kind === 'clients' ? 'Hostname' : 'Peer'}</th>
          <th>MAC</th>
          <th>Signal</th>
          {kind === 'clients' ? (
            <>
              <th className="num">Rx</th>
              <th className="num">Tx</th>
            </>
          ) : (
            <th className="num">Throughput</th>
          )}
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => (
          <tr key={i}>
            <td>{r.hostname || '—'}</td>
            <td className="mono">{r.macAddress}</td>
            <td>{r.signalDbm} dBm</td>
            {kind === 'clients' ? (
              <>
                <td className="num">{formatBitrate(r.rxRateBps)}</td>
                <td className="num">{formatBitrate(r.txRateBps)}</td>
              </>
            ) : (
              <td className="num">{r.throughputMbps?.toFixed(1) ?? '0.0'} Mbps</td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function formatBitrate(bps) {
  if (!bps) return '—';
  const mbps = Number(bps) / 1_000_000;
  if (mbps >= 1) return `${mbps.toFixed(1)} Mbps`;
  return `${(Number(bps) / 1_000).toFixed(0)} Kbps`;
}

function RadioCard({ radio }) {
  const [status, setStatus] = useState(null);
  const [original, setOriginal] = useState(null);
  const [draft, setDraft] = useState(null);
  const [availOpts, setAvailOpts] = useState({ channels: [], bandwidths: [], encryptions: [] });
  const [clients, setClients] = useState(null);
  const [peers, setPeers] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [error, setError] = useState(null);
  const [success, setSuccess] = useState(null);

  const isS1G = radio.band === BAND_S1G;

  const loadCard = useCallback(async () => {
    setLoading(true);
    try {
      const [statusRes, settingsRes] = await Promise.allSettled([
        wifiClient.getRadioStatus({ radioName: radio.name }),
        wifiClient.getRadioSettings({ radioName: radio.name }),
      ]);
      const statusVal = statusRes.status === 'fulfilled' ? statusRes.value.status : null;
      if (statusVal) setStatus(statusVal);
      if (settingsRes.status === 'fulfilled') {
        const s = seedTxPowerForDisplay(settingsRes.value.settings, statusVal);
        setOriginal(s);
        setDraft(s);
        setAvailOpts({
          channels: settingsRes.value.availableChannels ?? [],
          bandwidths: settingsRes.value.availableBandwidths ?? [],
          encryptions: settingsRes.value.availableEncryptions ?? [],
        });
      }
      setError(null);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [radio.name]);

  const loadConnected = useCallback(async () => {
    const [c, p] = await Promise.allSettled([
      wifiClient.listConnectedClients({ radioName: radio.name }),
      wifiClient.listMeshPeers({ radioName: radio.name }),
    ]);
    if (c.status === 'fulfilled') setClients(c.value.clients ?? []);
    if (p.status === 'fulfilled') setPeers(p.value.peers ?? []);
  }, [radio.name]);

  useEffect(() => {
    // Fetch-on-mount: load card details for this radio.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadCard();
  }, [loadCard]);
  useEffect(() => {
    // Fetch-on-expand: load clients/peers when the card opens.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (expanded) loadConnected();
  }, [expanded, loadConnected]);

  const update = (field, value) => {
    setDraft(prev => ({ ...prev, [field]: value }));
  };

  const updateMode = (newMode) => {
    setDraft(prev => {
      const next = { ...prev, mode: newMode };
      // When switching INTO mesh, seed mesh_id from ssid so the operator
      // doesn't have to retype the network name.
      if (isMeshMode(newMode) && !next.meshId) next.meshId = prev.ssid;
      return next;
    });
  };

  const save = async () => {
    if (!draft) return;
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      // For mesh mode, mirror mesh_id into ssid so the proto's ssid validation
      // (min_len 1) is satisfied with a meaningful value.
      const payload = { ...draft };
      if (isMeshMode(payload.mode) && payload.meshId) {
        payload.ssid = payload.meshId;
      }
      const resp = await wifiClient.updateRadioSettings({
        radioName: radio.name,
        settings: payload,
      });
      if (resp.success === false) throw new Error(resp.message || 'Update failed');
      setSuccess('Settings saved. Wireless subsystem reloading.');
      setTimeout(() => loadCard(), 2000);
    } catch (e) {
      setError('Save failed: ' + e.message);
    } finally {
      setSaving(false);
    }
  };

  const cancel = () => {
    setDraft(original);
    setError(null);
    setSuccess(null);
  };

  const toggleEnabled = () => {
    if (!draft) return;
    update('disabled', !(draft.disabled ?? false));
  };

  if (loading) {
    return (
      <div className="lat-panel radio-card">
        <div className="panel-head">
          <div className="radio-head-left">
            <div className="name">{radio.displayName || radio.name}</div>
            <div className="meta">{radio.hardwareName} · {BAND_LABELS[radio.band] || '?'}</div>
          </div>
        </div>
        <div className="loading-row">Loading…</div>
      </div>
    );
  }

  const dirty = !settingsEqual(original, draft);
  const enabled = !(draft?.disabled ?? false);
  const mesh = isMeshMode(draft?.mode);

  return (
    <div className={`lat-panel radio-card${enabled ? '' : ' disabled-card'}`}>
      <div className="panel-head">
        <div className="radio-head-left">
          <div className="name">{radio.displayName || radio.name}</div>
          <div className="meta">
            {radio.hardwareName} · {BAND_LABELS[radio.band] || '?'} · iface {radio.interfaceName}
          </div>
        </div>
        <div className="radio-head-right">
          <span className={`lat-chip ${status?.active ? 'ok' : 'crit'}`}>
            <span className="dot"></span>{status?.active ? 'Active' : 'Inactive'}
          </span>
          <button
            type="button"
            className={`lat-toggle${enabled ? ' on' : ''}`}
            aria-pressed={enabled}
            onClick={toggleEnabled}
          >
            <span className="track"><span className="thumb"></span></span>
            <span className="label">{enabled ? 'Enabled' : 'Disabled'}</span>
          </button>
        </div>
      </div>

      {error && <div className="lat-alert crit">{error}</div>}
      {success && <div className="lat-alert ok">{success}</div>}

      {status && (
        <div className="status-strip">
          <div className="kv"><span className="k">{mesh ? 'Mesh ID' : 'SSID'}</span><span className="v accent">{status.ssid || '—'}</span></div>
          <div className="kv"><span className="k">Mode</span><span className="v">{status.mode || '—'}</span></div>
          <div className="kv"><span className="k">Channel</span><span className="v">{status.channel || '—'}{status.frequency ? ` · ${status.frequency} MHz` : ''}</span></div>
          <div className="kv"><span className="k">Bandwidth</span><span className="v">{status.bandwidth || '—'}</span></div>
          <div className="kv"><span className="k">Encryption</span><span className="v">{status.encryption || '—'}</span></div>
          <div className="kv"><span className="k">Tx Power</span><span className="v accent">{status.txPower != null ? `${status.txPower} dBm` : '—'}</span></div>
          {mesh
            ? <div className="kv"><span className="k">Peers</span><span className="v">{status.meshPeers ?? 0}</span></div>
            : <div className="kv"><span className="k">Clients</span><span className="v">{status.connectedClients ?? 0}</span></div>}
        </div>
      )}

      {draft && (
        <div className="settings-grid">
          {!isS1G && (
            <div className="lat-field">
              <label>Mode</label>
              <LatSelect
                ariaLabel="Mode"
                value={draft.mode ?? WifiMode.UNSPECIFIED}
                onChange={updateMode}
                options={MODE_OPTIONS}
              />
            </div>
          )}

          <div className="lat-field">
            <label>{mesh ? 'Mesh ID' : 'SSID'}</label>
            <input
              className="lat-input"
              type="text"
              value={mesh ? (draft.meshId ?? '') : (draft.ssid ?? '')}
              onChange={e => update(mesh ? 'meshId' : 'ssid', e.target.value)}
            />
          </div>

          <div className="lat-field">
            <label>Password</label>
            <input
              className="lat-input"
              type="password"
              value={draft.password ?? ''}
              placeholder="(unchanged)"
              onChange={e => update('password', e.target.value)}
            />
          </div>

          <div className="lat-field">
            <label>Channel</label>
            <LatSelect
              ariaLabel="Channel"
              value={draft.channel ?? ''}
              onChange={v => update('channel', v)}
              options={
                availOpts.channels.length === 0
                  ? [{ value: draft.channel ?? '', label: draft.channel || '—' }]
                  : availOpts.channels.map(ch => ({ value: ch, label: String(ch) }))
              }
            />
          </div>

          <div className="lat-field">
            <label>Bandwidth</label>
            <LatSelect
              ariaLabel="Bandwidth"
              value={draft.bandwidth ?? 0}
              onChange={v => update('bandwidth', v)}
              options={
                availOpts.bandwidths.length === 0
                  ? [{ value: draft.bandwidth ?? 0, label: HT_MODE_LABELS[draft.bandwidth] || '—' }]
                  : availOpts.bandwidths.map(bw => ({ value: bw, label: HT_MODE_LABELS[bw] || String(bw) }))
              }
            />
          </div>

          <div className="lat-field">
            <label>Encryption</label>
            <LatSelect
              ariaLabel="Encryption"
              value={draft.encryption ?? 0}
              onChange={v => update('encryption', v)}
              options={availOpts.encryptions.map(enc => ({
                value: enc,
                label: ENCRYPTION_LABELS[enc] || String(enc),
              }))}
            />
          </div>

          <div className="lat-field">
            <label>Country</label>
            <input
              className="lat-input"
              type="text"
              maxLength={2}
              value={(draft.country ?? '').toUpperCase()}
              onChange={e => update('country', e.target.value.toUpperCase())}
            />
          </div>

          <PowerSelector
            valueDbm={draft.txPower}
            onChange={v => update('txPower', v)}
            hint="Saving will briefly reset the radio"
          />
        </div>
      )}

      {dirty && (
        <div className="actions-row">
          <button type="button" className="lat-btn primary" onClick={save} disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </button>
          <button type="button" className="lat-btn ghost" onClick={cancel} disabled={saving}>
            Cancel
          </button>
          <span className="lat-chip warn"><span className="dot"></span>Unsaved Changes</span>
        </div>
      )}

      <div className="disclosure">
        <button
          type="button"
          className="disclosure-head"
          aria-expanded={expanded}
          onClick={() => setExpanded(e => !e)}
        >
          <span className="caret">{expanded ? '▼' : '▶'}</span>
          {mesh ? 'Mesh Peers' : 'Connected Clients'}
          {expanded && (mesh ? (peers != null ? ` (${peers.length})` : '') : (clients != null ? ` (${clients.length})` : ''))}
        </button>
        {expanded && (
          <div className="disclosure-body">
            {mesh ? <ConnectedTable rows={peers} kind="peers" /> : <ConnectedTable rows={clients} kind="clients" />}
          </div>
        )}
      </div>
    </div>
  );
}

export default function SettingsWireless() {
  const [radios, setRadios] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const fetchRadios = useCallback(async () => {
    try {
      const resp = await wifiClient.listRadios({});
      // S1G first, then other bands by enum order. Stable across re-renders.
      const sorted = [...(resp.radios ?? [])].sort((a, b) => {
        if (a.band === BAND_S1G && b.band !== BAND_S1G) return -1;
        if (b.band === BAND_S1G && a.band !== BAND_S1G) return 1;
        return (a.band ?? 0) - (b.band ?? 0);
      });
      setRadios(sorted);
      setError(null);
    } catch (e) {
      setError('Failed to load radios: ' + e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // Fetch-on-mount: pull the radio list from the daemon.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchRadios();
  }, [fetchRadios]);

  return (
    <div className="settings-wireless">
      <h2 className="settings-h2">◇ Wireless Radios</h2>
      {error && <div className="settings-banner crit">{error}</div>}
      {loading ? (
        <div className="settings-loading"><div className="spinner"></div>Loading radios…</div>
      ) : radios.length === 0 ? (
        <div className="lat-panel"><div className="empty-row">No radios detected.</div></div>
      ) : (
        <div className="settings-wireless-list">
          {radios.map(r => <RadioCard key={r.name} radio={r} />)}
        </div>
      )}
    </div>
  );
}
