// =============================================================================
// WifiRadios.jsx — WiFi radio configuration page
// =============================================================================

import { useState, useEffect, useCallback } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { WifiConfigService } from "../gen/openmanet/wifi_config/v1/wifi_config_service_connect.js";
import { WifiMode } from "../gen/openmanet/wifi_config/v1/wifi_config_pb.js";

const wifiClient = createClient(WifiConfigService, transport);

const BAND_LABELS = { 0: '?', 1: '2.4 GHz', 2: '5 GHz', 3: '6 GHz', 4: 'Sub-1 GHz', 5: '60 GHz' };
const MODE_LABELS = { 0: '?', 1: 'AP', 2: 'Mesh', 3: 'Station', 4: 'Ad-Hoc', 5: 'Monitor' };

function encryptionLabel(val) {
  const map = { 0: '?', 1: 'SAE', 2: 'WPA2-PSK', 3: 'WPA-PSK', 4: 'PSK Mixed', 5: 'None', 6: 'OWE' };
  return map[val] ?? String(val);
}

function htModeLabel(val) {
  const map = {
    0: '?', 1: 'No HT', 2: 'HT20', 3: 'HT40-', 4: 'HT40+', 5: 'HT40',
    6: 'VHT20', 7: 'VHT40', 8: 'VHT80', 9: 'VHT160',
    10: 'HE20', 11: 'HE40', 12: 'HE80', 13: 'HE160',
    14: 'S1G 1MHz', 15: 'S1G 2MHz', 16: 'S1G 4MHz', 17: 'S1G 8MHz',
  };
  return map[val] ?? String(val);
}

function StatusBadge({ active }) {
  return (
    <span style={{
      display: 'inline-block', padding: '2px 8px', borderRadius: 4, fontSize: '0.75em', fontWeight: 600,
      background: active ? 'rgba(107,142,35,0.15)' : 'rgba(204,51,51,0.1)',
      color: active ? 'var(--green)' : 'var(--red)',
    }}>
      {active ? 'Active' : 'Inactive'}
    </span>
  );
}

function RadioCard({ radio }) {
  const [status, setStatus] = useState(null);
  const [settings, setSettings] = useState(null);
  const [availOpts, setAvailOpts] = useState(null);
  const [editSettings, setEditSettings] = useState(null);
  const [clients, setClients] = useState(null);
  const [peers, setPeers] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [error, setError] = useState(null);
  const [success, setSuccess] = useState(null);

  const fetchRadioData = useCallback(async () => {
    setLoading(true);
    try {
      const [statusRes, settingsRes] = await Promise.allSettled([
        wifiClient.getRadioStatus({ radioName: radio.name }),
        wifiClient.getRadioSettings({ radioName: radio.name }),
      ]);
      if (statusRes.status === 'fulfilled') setStatus(statusRes.value.status);
      if (settingsRes.status === 'fulfilled') {
        setSettings(settingsRes.value.settings);
        setEditSettings(null);
        setAvailOpts({
          channels: settingsRes.value.availableChannels ?? [],
          bandwidths: settingsRes.value.availableBandwidths ?? [],
          encryptions: settingsRes.value.availableEncryptions ?? [],
        });
      }
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [radio.name]);

  const fetchConnected = useCallback(async () => {
    const [clientsRes, peersRes] = await Promise.allSettled([
      wifiClient.listConnectedClients({ radioName: radio.name }),
      wifiClient.listMeshPeers({ radioName: radio.name }),
    ]);
    if (clientsRes.status === 'fulfilled') setClients(clientsRes.value.clients ?? []);
    if (peersRes.status === 'fulfilled') setPeers(peersRes.value.peers ?? []);
  }, [radio.name]);

  useEffect(() => { fetchRadioData(); }, [fetchRadioData]);

  useEffect(() => {
    if (expanded) fetchConnected();
  }, [expanded, fetchConnected]);

  const handleSave = async () => {
    if (!editSettings) return;
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const resp = await wifiClient.updateRadioSettings({
        radioName: radio.name,
        settings: editSettings,
      });
      if (resp.success === false) throw new Error(resp.message || 'Update failed');
      setSuccess('Settings saved. Wireless subsystem reloading.');
      setEditSettings(null);
      setTimeout(() => fetchRadioData(), 2000);
    } catch (e) {
      setError('Save failed: ' + e.message);
    } finally {
      setSaving(false);
    }
  };

  const current = editSettings || settings;
  const hasChanges = editSettings !== null;
  const isAP = status?.wifiMode === WifiMode.AP || status?.mode === 'ap';
  const isMesh = status?.wifiMode === WifiMode.MESH || status?.mode === 'mesh';

  const inputStyle = {
    background: 'var(--bg)', color: 'var(--text)', border: '1px solid var(--border)',
    borderRadius: 6, padding: '4px 8px', fontSize: '0.85em', outline: 'none', width: '100%',
  };
  const selectStyle = { ...inputStyle, cursor: 'pointer' };

  const startEdit = () => {
    if (!editSettings && settings) {
      setEditSettings({ ...settings });
    }
  };

  const updateField = (field, value) => {
    startEdit();
    setEditSettings(prev => ({ ...(prev || settings), [field]: value }));
  };

  return (
    <div className="card">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <div>
          <div className="card-title" style={{ margin: 0 }}>
            {radio.displayName || radio.name}
          </div>
          <div style={{ fontSize: '0.75em', color: 'var(--muted)' }}>
            {radio.hardwareName} &middot; {BAND_LABELS[radio.band] || '?'} &middot; {radio.interfaceName}
          </div>
        </div>
        {status && <StatusBadge active={status.active} />}
      </div>

      {error && (
        <div style={{ background: 'rgba(204,51,51,0.1)', border: '1px solid var(--red)', borderRadius: 6, padding: '6px 10px', marginBottom: 8, fontSize: '0.8em', color: 'var(--red)' }}>
          {error}
        </div>
      )}
      {success && (
        <div style={{ background: 'rgba(107,142,35,0.1)', border: '1px solid var(--green)', borderRadius: 6, padding: '6px 10px', marginBottom: 8, fontSize: '0.8em', color: 'var(--green)' }}>
          {success}
        </div>
      )}

      {loading ? (
        <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>Loading...</div>
      ) : (
        <>
          {/* Status row */}
          {status && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: 8, marginBottom: 10, fontSize: '0.82em' }}>
              <div><span style={{ color: 'var(--muted)' }}>SSID:</span> {status.ssid || '-'}</div>
              <div><span style={{ color: 'var(--muted)' }}>Mode:</span> {MODE_LABELS[status.wifiMode] || status.mode || '-'}</div>
              <div><span style={{ color: 'var(--muted)' }}>Ch:</span> {status.channel || '-'} ({status.frequency || '?'} MHz)</div>
              <div><span style={{ color: 'var(--muted)' }}>BW:</span> {status.bandwidth || '-'}</div>
              <div><span style={{ color: 'var(--muted)' }}>Enc:</span> {status.encryption || '-'}</div>
              <div><span style={{ color: 'var(--muted)' }}>TxPwr:</span> {status.txPower ?? '-'} dBm</div>
              {isAP && <div><span style={{ color: 'var(--muted)' }}>Clients:</span> {status.connectedClients ?? 0}</div>}
              {isMesh && <div><span style={{ color: 'var(--muted)' }}>Peers:</span> {status.meshPeers ?? 0}</div>}
            </div>
          )}

          {/* Editable settings */}
          {current && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 10, marginBottom: 10 }}>
              <div>
                <label style={{ fontSize: '0.78em', color: 'var(--muted)', display: 'block', marginBottom: 2 }}>SSID</label>
                <input style={inputStyle} value={current.ssid || ''} onChange={e => updateField('ssid', e.target.value)} />
              </div>
              {availOpts?.channels?.length > 0 && (
                <div>
                  <label style={{ fontSize: '0.78em', color: 'var(--muted)', display: 'block', marginBottom: 2 }}>Channel</label>
                  <select style={selectStyle} value={current.channel || ''} onChange={e => updateField('channel', e.target.value)}>
                    {availOpts.channels.map(ch => <option key={ch} value={ch}>{ch}</option>)}
                  </select>
                </div>
              )}
              {availOpts?.bandwidths?.length > 0 && (
                <div>
                  <label style={{ fontSize: '0.78em', color: 'var(--muted)', display: 'block', marginBottom: 2 }}>Bandwidth</label>
                  <select style={selectStyle} value={current.bandwidth ?? 0} onChange={e => updateField('bandwidth', Number(e.target.value))}>
                    {availOpts.bandwidths.map(bw => <option key={bw} value={bw}>{htModeLabel(bw)}</option>)}
                  </select>
                </div>
              )}
              {availOpts?.encryptions?.length > 0 && (
                <div>
                  <label style={{ fontSize: '0.78em', color: 'var(--muted)', display: 'block', marginBottom: 2 }}>Encryption</label>
                  <select style={selectStyle} value={current.encryption ?? 0} onChange={e => updateField('encryption', Number(e.target.value))}>
                    {availOpts.encryptions.map(enc => <option key={enc} value={enc}>{encryptionLabel(enc)}</option>)}
                  </select>
                </div>
              )}
              <div>
                <label style={{ fontSize: '0.78em', color: 'var(--muted)', display: 'block', marginBottom: 2 }}>Tx Power (dBm)</label>
                <input style={inputStyle} type="number" value={current.txPower ?? ''} onChange={e => updateField('txPower', Number(e.target.value))} />
              </div>
              <div>
                <label style={{ fontSize: '0.78em', color: 'var(--muted)', display: 'block', marginBottom: 2 }}>Password</label>
                <input style={inputStyle} type="password" value={current.password ?? ''} onChange={e => updateField('password', e.target.value)} placeholder="(unchanged)" />
              </div>
            </div>
          )}

          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {hasChanges && (
              <>
                <button onClick={handleSave} disabled={saving} style={{
                  padding: '6px 16px', border: 'none', borderRadius: 6, cursor: saving ? 'default' : 'pointer',
                  fontSize: '0.82em', fontWeight: 600, background: 'var(--accent)', color: 'var(--text)',
                  opacity: saving ? 0.5 : 1,
                }}>
                  {saving ? 'Saving...' : 'Save'}
                </button>
                <button onClick={() => setEditSettings(null)} style={{
                  padding: '6px 16px', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer',
                  fontSize: '0.82em', background: 'transparent', color: 'var(--muted)',
                }}>Cancel</button>
                <span style={{ fontSize: '0.78em', color: 'var(--yellow)' }}>Unsaved changes</span>
              </>
            )}
          </div>

          {/* Expandable clients/peers */}
          <div style={{ marginTop: 10 }}>
            <div onClick={() => setExpanded(!expanded)} style={{
              cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6,
              fontSize: '0.78em', color: 'var(--muted)',
            }}>
              <span style={{ fontSize: '0.75em' }}>{expanded ? '\u25BC' : '\u25B6'}</span>
              Connected Clients / Mesh Peers
            </div>
            {expanded && (
              <div style={{ marginTop: 6, fontSize: '0.8em' }}>
                {clients && clients.length > 0 ? (
                  <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 8 }}>
                    <thead>
                      <tr style={{ borderBottom: '1px solid var(--border)', color: 'var(--muted)', fontSize: '0.9em' }}>
                        <th style={{ textAlign: 'left', padding: '4px 6px' }}>Client</th>
                        <th style={{ textAlign: 'left', padding: '4px 6px' }}>MAC</th>
                        <th style={{ textAlign: 'right', padding: '4px 6px' }}>Signal</th>
                      </tr>
                    </thead>
                    <tbody>
                      {clients.map((c, i) => (
                        <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                          <td style={{ padding: '4px 6px' }}>{c.hostname || '-'}</td>
                          <td style={{ padding: '4px 6px', fontFamily: 'monospace', fontSize: '0.9em' }}>{c.macAddress}</td>
                          <td style={{ padding: '4px 6px', textAlign: 'right' }}>{c.signalDbm} dBm</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <div style={{ color: 'var(--muted)' }}>No connected clients</div>
                )}
                {peers && peers.length > 0 ? (
                  <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                      <tr style={{ borderBottom: '1px solid var(--border)', color: 'var(--muted)', fontSize: '0.9em' }}>
                        <th style={{ textAlign: 'left', padding: '4px 6px' }}>Peer</th>
                        <th style={{ textAlign: 'left', padding: '4px 6px' }}>MAC</th>
                        <th style={{ textAlign: 'right', padding: '4px 6px' }}>Signal</th>
                        <th style={{ textAlign: 'right', padding: '4px 6px' }}>Throughput</th>
                      </tr>
                    </thead>
                    <tbody>
                      {peers.map((p, i) => (
                        <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                          <td style={{ padding: '4px 6px' }}>{p.hostname || '-'}</td>
                          <td style={{ padding: '4px 6px', fontFamily: 'monospace', fontSize: '0.9em' }}>{p.macAddress}</td>
                          <td style={{ padding: '4px 6px', textAlign: 'right' }}>{p.signalDbm} dBm</td>
                          <td style={{ padding: '4px 6px', textAlign: 'right' }}>{p.throughputMbps?.toFixed(1)} Mbps</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <div style={{ color: 'var(--muted)' }}>No mesh peers</div>
                )}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}

export default function WifiRadiosPage() {
  const [radios, setRadios] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const fetchRadios = useCallback(async () => {
    try {
      const resp = await wifiClient.listRadios({});
      setRadios(resp.radios ?? []);
      setError(null);
    } catch (e) {
      setError('Failed to load radios: ' + e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchRadios(); }, [fetchRadios]);

  if (loading) {
    return (
      <div style={{ width: '100%', maxWidth: 900 }}>
        <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>WiFi Radios</h2>
        <div className="card" style={{ textAlign: 'center', padding: 40 }}>
          <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>Loading radios...</div>
        </div>
      </div>
    );
  }

  return (
    <div style={{ width: '100%', maxWidth: 900 }}>
      <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>WiFi Radios</h2>
      {error && (
        <div style={{ background: 'rgba(204,51,51,0.1)', border: '1px solid var(--red)', borderRadius: 6, padding: '8px 12px', marginBottom: 8, fontSize: '0.85em', color: 'var(--red)' }}>
          {error}
        </div>
      )}
      {radios.length === 0 ? (
        <div className="card" style={{ color: 'var(--muted)', fontSize: '0.85em' }}>No radios detected.</div>
      ) : (
        <div style={{ display: 'grid', gap: 8 }}>
          {radios.map(r => <RadioCard key={r.name} radio={r} onRefresh={fetchRadios} />)}
        </div>
      )}
    </div>
  );
}
