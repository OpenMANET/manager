// =============================================================================
// BLOS.jsx — Beyond Line of Sight (VPN/Tailscale) configuration page
// =============================================================================

import { useState, useEffect, useCallback } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { BLOSService } from "../gen/openmanet/blos/v1/blos_service_connect.js";

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

  const inputStyle = {
    background: 'var(--bg)', color: 'var(--text)', border: '1px solid var(--border)',
    borderRadius: 6, padding: '6px 10px', fontSize: '0.88em', outline: 'none', width: '100%', maxWidth: 350,
  };
  const btnStyle = {
    padding: '8px 20px', border: 'none', borderRadius: 6, cursor: 'pointer',
    fontSize: '0.85em', fontWeight: 600, background: 'var(--accent)', color: 'var(--text)',
  };

  if (loading) {
    return (
      <div style={{ width: '100%', maxWidth: 900 }}>
        <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>BLOS (Beyond Line of Sight)</h2>
        <div className="card" style={{ textAlign: 'center', padding: 40 }}>
          <div style={{ color: 'var(--muted)', fontSize: '0.85em' }}>Loading BLOS status...</div>
        </div>
      </div>
    );
  }

  return (
    <div style={{ width: '100%', maxWidth: 900 }}>
      <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>BLOS (Beyond Line of Sight)</h2>

      {error && (
        <div style={{ background: 'rgba(204,51,51,0.1)', border: '1px solid var(--red)', borderRadius: 6, padding: '8px 12px', marginBottom: 8, fontSize: '0.85em', color: 'var(--red)' }}>
          {error}
        </div>
      )}
      {success && (
        <div style={{ background: 'rgba(107,142,35,0.1)', border: '1px solid var(--green)', borderRadius: 6, padding: '8px 12px', marginBottom: 8, fontSize: '0.85em', color: 'var(--green)' }}>
          {success}
        </div>
      )}

      <div className="card">
        <div className="card-title">BLOS Status</div>
        {status && (
          <div style={{ fontSize: '0.9em', marginBottom: 10 }}>
            <span style={{ color: 'var(--muted)' }}>Status:</span>{' '}
            <span style={{ color: status.blosEnabled ? 'var(--green)' : 'var(--red)', fontWeight: 600 }}>
              {status.blosEnabled ? 'Enabled' : 'Disabled'}
            </span>
            {status.message && <span style={{ color: 'var(--muted)', marginLeft: 8 }}>({status.message})</span>}
          </div>
        )}
      </div>

      <div className="card" style={{ marginTop: 8 }}>
        <div className="card-title">Configuration</div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(250px, 1fr))', gap: 12 }}>
          <div>
            <label style={{ fontSize: '0.82em', color: 'var(--muted)', display: 'block', marginBottom: 4 }}>Enable BLOS</label>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: '0.88em' }}>
              <div
                onClick={() => setEnableBlos(!enableBlos)}
                style={{
                  width: 42, height: 22, borderRadius: 11, padding: 2,
                  background: enableBlos ? 'var(--green)' : 'var(--border)',
                  cursor: 'pointer', transition: 'background 0.2s', display: 'flex', alignItems: 'center',
                }}
              >
                <div style={{
                  width: 18, height: 18, borderRadius: '50%', background: '#fff',
                  transition: 'transform 0.2s',
                  transform: enableBlos ? 'translateX(20px)' : 'translateX(0)',
                }} />
              </div>
              {enableBlos ? 'On' : 'Off'}
            </label>
          </div>
          <div>
            <label style={{ fontSize: '0.82em', color: 'var(--muted)', display: 'block', marginBottom: 4 }}>Auth Key</label>
            <input style={inputStyle} type="password" value={authKey} onChange={e => setAuthKey(e.target.value)} placeholder="Paste auth key" />
          </div>
          <div>
            <label style={{ fontSize: '0.82em', color: 'var(--muted)', display: 'block', marginBottom: 4 }}>Login Server URL (optional)</label>
            <input style={inputStyle} value={loginServer} onChange={e => setLoginServer(e.target.value)} placeholder="https://headscale.example.com" />
          </div>
        </div>

        <div style={{ marginTop: 12 }}>
          <button onClick={handleSave} disabled={saving} style={{ ...btnStyle, opacity: saving ? 0.5 : 1 }}>
            {saving ? 'Saving...' : 'Update BLOS Config'}
          </button>
        </div>
      </div>
    </div>
  );
}
