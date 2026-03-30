// =============================================================================
// Settings.jsx — Device settings page
// =============================================================================

import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { createClient } from "@connectrpc/connect";
import { transport } from "../services/connectClient.js";
import { DashboardService } from "../gen/openmanet/dashboard/v1/dashboard_service_connect.js";
import { QuickAction } from "../gen/openmanet/dashboard/v1/dashboard_pb.js";

const dashboardClient = createClient(DashboardService, transport);

// Simple YAML formatter for preview display (no external library needed).
function formatYaml(obj, indent) {
  if (!obj || typeof obj !== 'object') return '';
  const pad = '  '.repeat(indent);
  let out = '';
  for (const [key, val] of Object.entries(obj)) {
    if (val === null || val === undefined) {
      out += `${pad}${key}:\n`;
    } else if (Array.isArray(val)) {
      out += `${pad}${key}:\n`;
      val.forEach((item) => { out += `${pad}  - ${item}\n`; });
    } else if (typeof val === 'object') {
      out += `${pad}${key}:\n${formatYaml(val, indent + 1)}`;
    } else {
      out += `${pad}${key}: ${val}\n`;
    }
  }
  return out;
}

export default function SettingsPage() {
  const [hostname, setHostname] = useState('');
  const [originalHostname, setOriginalHostname] = useState('');
  const [config, setConfig] = useState({
    comms_enabled: true,
    control_source: 'web',
    debug: false,
    talkgroup_count: 5,
  });
  const [originalConfig, setOriginalConfig] = useState(null);
  const [fullConfig, setFullConfig] = useState(null);
  const [showRaw, setShowRaw] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [error, setError] = useState(null);
  const [success, setSuccess] = useState(null);

  const fetchSettings = useCallback(async () => {
    try {
      const results = await Promise.allSettled([
        fetch('/api/settings/hostname').then(r => r.ok ? r.json() : Promise.reject(r.status)),
        fetch('/api/settings/config').then(r => r.ok ? r.json() : Promise.reject(r.status)),
      ]);

      if (results[0].status === 'fulfilled') {
        const h = results[0].value.hostname || results[0].value.name || '';
        setHostname(h);
        setOriginalHostname(h);
      }

      if (results[1].status === 'fulfilled') {
        const data = results[1].value;
        // The API returns the full YAML config as nested JSON.
        // Extract the fields we expose in the UI.
        const comms = data.comms || {};
        const cfg = {
          comms_enabled: comms.enable ?? true,
          control_source: comms.controlSource ?? 'web',
          debug: comms.debug ?? false,
          talkgroup_count: comms.talkgroups ?? 5,
        };
        setConfig(cfg);
        setOriginalConfig(cfg);
        setFullConfig(data);
      }

      setError(null);
    } catch (e) {
      setError('Failed to load settings: ' + e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchSettings();
  }, [fetchSettings]);

  // Build a live YAML preview by merging current UI state into the full config.
  const liveYaml = useMemo(() => {
    if (!fullConfig) return '';
    const merged = JSON.parse(JSON.stringify(fullConfig));
    if (!merged.comms) merged.comms = {};
    merged.comms.enable = config.comms_enabled;
    merged.comms.controlSource = config.control_source;
    merged.comms.debug = config.debug;
    merged.comms.talkgroups = config.talkgroup_count;
    return formatYaml(merged, 0);
  }, [fullConfig, config]);

  const clearMessages = () => { setError(null); setSuccess(null); };

  const handleSaveHostname = async () => {
    clearMessages();
    setSaving(true);
    try {
      const res = await fetch('/api/settings/hostname', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hostname }),
      });
      if (!res.ok) throw new Error('HTTP ' + res.status);
      setOriginalHostname(hostname);
      setSuccess('Hostname saved.');
    } catch (e) {
      setError('Failed to save hostname: ' + e.message);
    } finally {
      setSaving(false);
    }
  };

  const handleSaveConfig = async () => {
    clearMessages();
    setSaving(true);
    try {
      // Send nested structure matching the YAML config format.
      // The backend deep-merges this into the existing config.
      const payload = {
        comms: {
          enable: config.comms_enabled,
          controlSource: config.control_source,
          debug: config.debug,
          talkgroups: config.talkgroup_count,
        },
      };
      const res = await fetch('/api/settings/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!res.ok) throw new Error('HTTP ' + res.status);
      setOriginalConfig({ ...config });
      setSuccess('Configuration saved.');
    } catch (e) {
      setError('Failed to save config: ' + e.message);
    } finally {
      setSaving(false);
    }
  };

  const handleRestart = async () => {
    setRestarting(true);
    clearMessages();
    try {
      const resp = await dashboardClient.executeQuickAction({
        action: QuickAction.RESTART_OPENMANETD,
      });
      if (!resp.success) throw new Error(resp.message || 'action failed');
      setSuccess('openmanetd restart requested.');
    } catch (e) {
      setError('Failed to restart: ' + e.message);
    } finally {
      setTimeout(() => setRestarting(false), 2000);
    }
  };

  const inputStyle = {
    background: 'var(--bg)', color: 'var(--text)', border: '1px solid var(--border)',
    borderRadius: 6, padding: '6px 10px', fontSize: '0.88em', outline: 'none',
    width: '100%', maxWidth: 300,
  };
  const selectStyle = { ...inputStyle, cursor: 'pointer' };
  const btnStyle = {
    padding: '8px 20px', border: 'none', borderRadius: 6, cursor: 'pointer',
    fontSize: '0.85em', fontWeight: 600, transition: 'opacity 0.15s',
  };
  const primaryBtn = { ...btnStyle, background: 'var(--accent)', color: 'var(--text)' };
  const dangerBtn = { ...btnStyle, background: 'rgba(204,51,51,0.15)', border: '1px solid var(--red)', color: 'var(--red)' };

  if (loading) {
    return (
      <div style={{ width: '100%', maxWidth: 900 }}>
        <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>Settings</h2>
        <div className="card" style={{ textAlign: 'center', padding: 40 }}>
          <div className="spinner" />
          <div style={{ color: 'var(--muted)', marginTop: 10, fontSize: '0.85em' }}>Loading settings...</div>
        </div>
        <style>{`
          .spinner { width:28px;height:28px;margin:0 auto;border:3px solid var(--border);border-top-color:var(--accent);border-radius:50%;animation:spin 0.7s linear infinite; }
          @keyframes spin { to { transform:rotate(360deg); } }
        `}</style>
      </div>
    );
  }

  const hostnameChanged = hostname !== originalHostname;
  const configChanged = originalConfig && (
    config.comms_enabled !== originalConfig.comms_enabled ||
    config.control_source !== originalConfig.control_source ||
    config.debug !== originalConfig.debug ||
    config.talkgroup_count !== originalConfig.talkgroup_count
  );

  return (
    <div style={{ width: '100%', maxWidth: 900 }}>
      <h2 style={{ fontSize: '1.2em', marginBottom: 12 }}>Settings</h2>

      {/* Status messages */}
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

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 8 }}>

        {/* Hostname */}
        <div className="card">
          <div className="card-title">Hostname</div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <input
              type="text"
              value={hostname}
              onChange={(e) => setHostname(e.target.value)}
              placeholder="device-hostname"
              style={inputStyle}
            />
            <button
              onClick={handleSaveHostname}
              disabled={saving || !hostnameChanged}
              style={{
                ...primaryBtn,
                opacity: saving || !hostnameChanged ? 0.5 : 1,
                cursor: saving || !hostnameChanged ? 'default' : 'pointer',
              }}
            >
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>

        {/* Service Control */}
        <div className="card">
          <div className="card-title">Service Control</div>
          <button
            onClick={handleRestart}
            disabled={restarting}
            style={{ ...dangerBtn, opacity: restarting ? 0.5 : 1 }}
          >
            {restarting ? 'Restarting...' : 'Restart openmanetd'}
          </button>
        </div>

        {/* OpenMANETd Config */}
        <div className="card" style={{ gridColumn: '1 / -1' }}>
          <div className="card-title">OpenMANETd Configuration</div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(250px, 1fr))', gap: 16 }}>

            {/* Comms Enable */}
            <div>
              <label style={{ fontSize: '0.82em', color: 'var(--muted)', display: 'block', marginBottom: 4 }}>
                Comms Radio
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: '0.88em' }}>
                <div
                  onClick={() => setConfig(c => ({ ...c, comms_enabled: !c.comms_enabled }))}
                  style={{
                    width: 42, height: 22, borderRadius: 11, padding: 2,
                    background: config.comms_enabled ? 'var(--green)' : 'var(--border)',
                    cursor: 'pointer', transition: 'background 0.2s', display: 'flex', alignItems: 'center',
                  }}
                >
                  <div style={{
                    width: 18, height: 18, borderRadius: '50%', background: '#fff',
                    transition: 'transform 0.2s',
                    transform: config.comms_enabled ? 'translateX(20px)' : 'translateX(0)',
                  }} />
                </div>
                {config.comms_enabled ? 'Enabled' : 'Disabled'}
              </label>
            </div>

            {/* Control Source */}
            <div>
              <label style={{ fontSize: '0.82em', color: 'var(--muted)', display: 'block', marginBottom: 4 }}>
                Control Source
              </label>
              <select
                value={config.control_source}
                onChange={(e) => setConfig(c => ({ ...c, control_source: e.target.value }))}
                style={selectStyle}
              >
                <option value="web">Web UI</option>
                <option value="cm108">CM108 (USB GPIO)</option>
                <option value="nanoPTT">nanoPTT</option>
              </select>
            </div>

            {/* Debug Toggle */}
            <div>
              <label style={{ fontSize: '0.82em', color: 'var(--muted)', display: 'block', marginBottom: 4 }}>
                Debug Mode
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: '0.88em' }}>
                <div
                  onClick={() => setConfig(c => ({ ...c, debug: !c.debug }))}
                  style={{
                    width: 42, height: 22, borderRadius: 11, padding: 2,
                    background: config.debug ? 'var(--yellow)' : 'var(--border)',
                    cursor: 'pointer', transition: 'background 0.2s', display: 'flex', alignItems: 'center',
                  }}
                >
                  <div style={{
                    width: 18, height: 18, borderRadius: '50%', background: '#fff',
                    transition: 'transform 0.2s',
                    transform: config.debug ? 'translateX(20px)' : 'translateX(0)',
                  }} />
                </div>
                {config.debug ? 'On' : 'Off'}
              </label>
            </div>

          </div>

          <div style={{ marginTop: 14, display: 'flex', gap: 8, alignItems: 'center' }}>
            <button
              onClick={handleSaveConfig}
              disabled={saving || !configChanged}
              style={{
                ...primaryBtn,
                opacity: saving || !configChanged ? 0.5 : 1,
                cursor: saving || !configChanged ? 'default' : 'pointer',
              }}
            >
              {saving ? 'Saving...' : 'Save Configuration'}
            </button>
            {configChanged && (
              <span style={{ fontSize: '0.78em', color: 'var(--yellow)' }}>Unsaved changes</span>
            )}
          </div>
        </div>

        {/* Raw Config View */}
        <div className="card" style={{ gridColumn: '1 / -1' }}>
          <div
            onClick={() => setShowRaw(!showRaw)}
            style={{
              cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6,
              fontSize: '0.82em', color: 'var(--muted)',
            }}
          >
            <span style={{ fontSize: '0.75em' }}>{showRaw ? '\u25BC' : '\u25B6'}</span>
            <span className="card-title" style={{ margin: 0 }}>Raw Configuration (YAML)</span>
          </div>
          {showRaw && (
            <pre style={{
              background: '#0a0d10', border: '1px solid var(--border)', borderRadius: 6,
              padding: 10, fontSize: '0.75em', lineHeight: 1.5, color: '#8b949e',
              maxHeight: 400, overflowY: 'auto', marginTop: 8, whiteSpace: 'pre-wrap',
              fontFamily: "'Courier New', monospace",
              WebkitUserSelect: 'text', userSelect: 'text',
            }}>
              {liveYaml || 'No configuration data available.'}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}
