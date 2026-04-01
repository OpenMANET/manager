// =============================================================================
// LoginPage.jsx — Authentication gate for the OpenMANET WebUI
// =============================================================================

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { createClient } from '@connectrpc/connect';
import { DashboardService } from '../gen/openmanet/dashboard/v1/dashboard_service_connect.js';
import { transport } from '../services/connectClient.js';
import { useAuth } from '../contexts/useAuth.js';
import './LoginPage.css';

const dashboardClient = createClient(DashboardService, transport);

export default function LoginPage() {
  const { login, isAuthenticated } = useAuth();
  const navigate = useNavigate();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [deviceInfo, setDeviceInfo] = useState(null);

  // Redirect if already authenticated.
  useEffect(() => {
    if (isAuthenticated) navigate('/', { replace: true });
  }, [isAuthenticated, navigate]);

  // Fetch device info for the footer (unauthenticated, exempt from auth middleware).
  useEffect(() => {
    dashboardClient.getDashboardStatus({})
      .then(resp => setDeviceInfo(resp.deviceInfo ?? null))
      .catch(() => { /* best-effort — footer just stays empty */ });
  }, []);

  async function handleSubmit(e) {
    e.preventDefault();

    if (!username || !password) {
      setError('Please enter username and password');
      return;
    }

    setError('');
    setSubmitting(true);

    try {
      await login(username, password);
      navigate('/', { replace: true });
    } catch (err) {
      setError(err.message || 'Login failed');
    } finally {
      setSubmitting(false);
    }
  }

  const footerText = buildFooterText(deviceInfo);

  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-header">
          <span className="login-icon" aria-hidden="true">📡</span>
          <h1 className="login-title">OpenMANET</h1>
          <p className="login-subtitle">OpenMANET Management Interface</p>
        </div>

        <form className="login-form" onSubmit={handleSubmit} noValidate>
          <div className="login-field">
            <label htmlFor="username">Username</label>
            <input
              id="username"
              type="text"
              autoComplete="username"
              value={username}
              onChange={e => setUsername(e.target.value)}
              disabled={submitting}
            />
          </div>

          <div className="login-field">
            <label htmlFor="password">Password</label>
            <input
              id="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              disabled={submitting}
            />
          </div>

          {error && (
            <div className="login-error" role="alert">{error}</div>
          )}

          <button
            type="submit"
            className="login-btn"
            disabled={submitting}
          >
            {submitting ? 'Signing in…' : 'Sign In'}
          </button>
        </form>

        {footerText && (
          <p className="login-footer">{footerText}</p>
        )}
      </div>
    </div>
  );
}

function buildFooterText(deviceInfo) {
  if (!deviceInfo) return null;

  const parts = [];

  if (deviceInfo.firmware) parts.push(deviceInfo.firmware);
  if (deviceInfo.model) parts.push(deviceInfo.model);

  return parts.length > 0 ? parts.join(' \u2014 ') : null;
}
