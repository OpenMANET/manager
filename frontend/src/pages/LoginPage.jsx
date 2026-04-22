// =============================================================================
// LoginPage.jsx — Operator authentication terminal
// =============================================================================

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { createClient } from '@connectrpc/connect';
import { DashboardService } from '../gen/openmanet/dashboard/v1/dashboard_service_connect.js';
import { transport } from '../services/connectClient.js';
import { useAuth } from '../contexts/useAuth.js';
import './LoginPage.css';

const dashboardClient = createClient(DashboardService, transport);

function useUtcClock() {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}

function formatUtc(d) {
  return d.toISOString().slice(11, 19);
}
function formatDate(d) {
  return d.toISOString().slice(0, 10);
}

export default function LoginPage() {
  const { login, isAuthenticated } = useAuth();
  const navigate = useNavigate();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [deviceInfo, setDeviceInfo] = useState(null);
  const now = useUtcClock();

  // Redirect if already authenticated.
  useEffect(() => {
    if (isAuthenticated) navigate('/', { replace: true });
  }, [isAuthenticated, navigate]);

  // Best-effort unauthenticated device-info fetch for the corner readouts.
  useEffect(() => {
    dashboardClient.getDashboardStatus({})
      .then(resp => setDeviceInfo(resp.deviceInfo ?? null))
      .catch(() => { /* empty state is fine — corners just show em-dashes */ });
  }, []);

  async function handleSubmit(e) {
    e.preventDefault();

    if (!username || !password) {
      setError('Please enter operator and passphrase');
      return;
    }

    setError('');
    setSubmitting(true);

    try {
      await login(username, password);
      navigate('/', { replace: true });
    } catch (err) {
      setError(err.message || 'Authentication failed');
    } finally {
      setSubmitting(false);
    }
  }

  const hostname = deviceInfo?.hostname || '—';
  const firmware = deviceInfo?.firmware || '—';
  const model = deviceInfo?.model || '—';
  const kernel = deviceInfo?.kernel || '';
  const arch = deviceInfo?.architecture || '';

  return (
    <div className="login-screen">
      <div className="login-corner tl">
        NODE<br/><span className="v">{hostname}</span>
      </div>
      <div className="login-corner tr">
        {formatDate(now)}<br/><span className="v">{formatUtc(now)} UTC</span>
      </div>
      <div className="login-corner bl">
        OPENMANETD<br/><span className="v">{firmware}</span>
      </div>
      <div className="login-corner br">
        {model}<br/><span className="v">{[kernel, arch].filter(Boolean).join(' · ') || '—'}</span>
      </div>

      <div className="login-card">
        <div className="login-mark">◇ OPENMANET</div>
        <div className="login-sub">Mesh Operator Terminal</div>

        <form className="login-form" onSubmit={handleSubmit} noValidate>
          <div className="lat-field">
            <label htmlFor="username">Operator</label>
            <input
              id="username"
              className="lat-input"
              type="text"
              autoComplete="username"
              value={username}
              onChange={e => setUsername(e.target.value)}
              disabled={submitting}
            />
          </div>

          <div className="lat-field">
            <label htmlFor="password">Passphrase</label>
            <input
              id="password"
              className="lat-input"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              disabled={submitting}
            />
          </div>

          {error && (
            <div className="lat-alert crit" role="alert">{error}</div>
          )}

          <button
            type="submit"
            className="lat-btn primary login-submit"
            disabled={submitting}
          >
            {submitting ? 'AUTHENTICATING…' : '◇ AUTHENTICATE'}
          </button>
        </form>
      </div>
    </div>
  );
}
