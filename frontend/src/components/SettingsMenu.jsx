// =============================================================================
// SettingsMenu.jsx — Hamburger menu with panel visibility toggles
// =============================================================================

import React, { useState, useRef, useEffect } from 'react';

const PANEL_LABELS = {
  channels: 'Channels',
  ptt: 'PTT Button',
  waveform: 'RX Waveform',
  micMeter: 'Mic Meter',
  audioControls: 'Audio Controls',
  fileTx: 'Audio File TX',
  meshStatus: 'Mesh Status',
  topology: 'Network Topology',
  transcript: 'Transcript',
  log: 'System Log',
};

export const ALL_PANELS = Object.keys(PANEL_LABELS);

export default function SettingsMenu({ panelVisibility, onTogglePanel }) {
  const [open, setOpen] = useState(false);
  const menuRef = useRef(null);

  // Close menu when clicking outside.
  useEffect(() => {
    if (!open) return;
    const handler = (e) => {
      if (menuRef.current && !menuRef.current.contains(e.target)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    document.addEventListener('touchstart', handler);
    return () => {
      document.removeEventListener('mousedown', handler);
      document.removeEventListener('touchstart', handler);
    };
  }, [open]);

  return (
    <div className="settings-menu" ref={menuRef}>
      <button
        className="hamburger-btn"
        onClick={() => setOpen((v) => !v)}
        aria-label="Settings"
      >
        <span className="hamburger-icon" />
        <span className="hamburger-icon" />
        <span className="hamburger-icon" />
      </button>

      {open && (
        <div className="settings-dropdown">
          <div className="settings-title">Panels</div>
          {ALL_PANELS.map((key) => (
            <label key={key} className="settings-item">
              <input
                type="checkbox"
                checked={panelVisibility[key] !== false}
                onChange={() => onTogglePanel(key)}
              />
              {PANEL_LABELS[key]}
            </label>
          ))}
        </div>
      )}
    </div>
  );
}
