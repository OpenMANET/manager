// =============================================================================
// StatusBar.jsx — WebSocket and Audio connection status indicators
// =============================================================================
// Displays colored dots and labels for WebSocket and Audio status.
// Props:
//   wsStatus    — 'connected' | 'connecting' | 'disconnected'
//   audioStatus — string describing audio state (e.g., 'Audio off', 'Audio ready')

import React from 'react';

// Maps a status string to the appropriate CSS dot class.
function dotClass(status) {
  if (status === 'connected') return 'status-dot dot-green';
  if (status === 'connecting') return 'status-dot dot-yellow';
  return 'status-dot dot-red';
}

// Maps a status string to a human-readable label.
function statusLabel(status) {
  if (status === 'connected') return 'Connected';
  if (status === 'connecting') return 'Connecting...';
  return 'Disconnected';
}

export default React.memo(function StatusBar({ wsStatus, audioStatus }) {
  return (
    <div className="status-bar">
      <div>
        <span className={dotClass(wsStatus)} />
        <span>{statusLabel(wsStatus)}</span>
      </div>
      <div>
        <span className={audioStatus === 'Audio ready' ? 'status-dot dot-green' : 'status-dot dot-red'} />
        <span>{audioStatus}</span>
      </div>
    </div>
  );
});
