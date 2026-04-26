// =============================================================================
// SettingsTerminal.jsx — Web TTY terminal page
// =============================================================================
//
// Hosts the dumb <Terminal /> component, builds the WS URL from the current
// origin, surfaces a "session in use" banner on close code 1008, and exposes
// a Reconnect button that remounts the terminal (forcing a fresh WS).

import React, { useState, useCallback, useMemo } from 'react';
import Terminal from '../components/Terminal.jsx';
import './SettingsTerminal.css';

function buildWsUrl() {
  const proto = typeof window !== 'undefined' && window.location.protocol === 'https:' ? 'wss' : 'ws';
  const host = typeof window !== 'undefined' ? window.location.host : 'localhost';
  return `${proto}://${host}/api/terminal/ws`;
}

export default function SettingsTerminal() {
  const [closedEvent, setClosedEvent] = useState(null);
  const [reconnectKey, setReconnectKey] = useState(0);

  const wsUrl = useMemo(buildWsUrl, []);

  const handleClose = useCallback((e) => {
    setClosedEvent({ code: e?.code, reason: e?.reason || '' });
  }, []);

  const reconnect = useCallback(() => {
    setClosedEvent(null);
    setReconnectKey((k) => k + 1);
  }, []);

  return (
    <div className="settings-terminal">
      <div className="settings-terminal-toolbar">
        <h3>Terminal</h3>
        <span className="settings-terminal-hint">
          Session ends when you leave this page or close the tab.
        </span>
      </div>

      {closedEvent ? (
        <div className="settings-terminal-banner">
          {closedEvent.code === 1008 ? (
            <span>Terminal session is already in use elsewhere. Close the other session and try Reconnect.</span>
          ) : (
            <span>
              Session closed (code {closedEvent.code}{closedEvent.reason ? `: ${closedEvent.reason}` : ''}).
            </span>
          )}
          <button type="button" className="lat-btn primary" onClick={reconnect}>Reconnect</button>
        </div>
      ) : (
        <div className="settings-terminal-host">
          <Terminal key={reconnectKey} wsUrl={wsUrl} onClose={handleClose} />
        </div>
      )}
    </div>
  );
}
