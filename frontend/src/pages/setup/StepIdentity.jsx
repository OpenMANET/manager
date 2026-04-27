// =============================================================================
// StepIdentity.jsx — Hostname + role selection (combined Step 1)
// =============================================================================
//
// Owns profile.hostname and profile.role. Validates the hostname against
// the same RFC 1123 pattern the backend's phase 1 check enforces, so the
// Next button stays disabled until the input would pass the server-side
// validator. When status.alreadyConfigured is true the user is shown a
// warning banner and the Next button is double-confirmed.

import { useState } from 'react';
import { useSetup, SETUP_ACTIONS } from '../../contexts/SetupContext.jsx';
import { MeshRole } from '../../gen/openmanet/setup/v1/setup_pb.js';
import { ROLE_LABELS } from './labels.js';

const HOSTNAME_RE = /^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$/;

function isValidHostname(s) {
  if (!s) return false;
  if (s.length > 63) return false;
  return HOSTNAME_RE.test(s);
}

export default function StepIdentity({ status, onAdvance }) {
  const { state, dispatch } = useSetup();
  const [confirmOpen, setConfirmOpen] = useState(false);

  const hostnameValid = isValidHostname(state.hostname);
  const roleValid = state.role !== MeshRole.UNSPECIFIED;
  const canAdvance = hostnameValid && roleValid;

  const handleNext = () => {
    if (status?.alreadyConfigured) {
      setConfirmOpen(true);
      return;
    }
    onAdvance?.();
  };

  return (
    <div className="setup-step">
      <h3>Identity &amp; Role</h3>

      {status?.alreadyConfigured && (
        <div className="lat-alert warn" role="alert">
          This device looks like it has already been configured.
          Continuing will reset wireless, network, firewall, DHCP, and
          batman configuration.
        </div>
      )}

      <div className="lat-field">
        <label htmlFor="setup-hostname">Hostname</label>
        <input
          id="setup-hostname"
          type="text"
          className="lat-input"
          value={state.hostname}
          onChange={(e) => dispatch({ type: SETUP_ACTIONS.SET_HOSTNAME, value: e.target.value })}
          placeholder="e.g. openmanet-1"
          autoComplete="off"
          spellCheck={false}
          maxLength={63}
        />
        {state.hostname && !hostnameValid && (
          <div className="setup-error">
            Hostname must be 1–63 alphanumeric chars or hyphens (no leading or trailing hyphen).
          </div>
        )}
        <div className="setup-help">
          Used as the device&apos;s mDNS name (e.g. <code>{state.hostname || 'openmanet-1'}.local</code>).
        </div>
      </div>

      <div className="setup-field">
        <label>Role</label>
        <div className="setup-role-cards" role="radiogroup" aria-label="Mesh node role">
          <RoleCard
            value={MeshRole.MESH_POINT}
            current={state.role}
            label={ROLE_LABELS[MeshRole.MESH_POINT]}
            description="A node that participates in the mesh and (optionally) extends Wi-Fi access to nearby clients."
            onSelect={(v) => dispatch({ type: SETUP_ACTIONS.SET_ROLE, value: v })}
          />
          <RoleCard
            value={MeshRole.MESH_GATE}
            current={state.role}
            label={ROLE_LABELS[MeshRole.MESH_GATE]}
            description="A node that bridges the mesh to a wired or upstream Wi-Fi network."
            onSelect={(v) => dispatch({ type: SETUP_ACTIONS.SET_ROLE, value: v })}
          />
        </div>
      </div>

      <div className="setup-nav">
        <button
          type="button"
          className="lat-btn primary"
          disabled={!canAdvance}
          onClick={handleNext}
        >
          Next
        </button>
      </div>

      {confirmOpen && (
        <ConfirmDialog
          onCancel={() => setConfirmOpen(false)}
          onConfirm={() => { setConfirmOpen(false); onAdvance?.(); }}
        />
      )}
    </div>
  );
}

function RoleCard({ value, current, label, description, onSelect }) {
  const selected = current === value;
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      className={`setup-role-card ${selected ? 'selected' : ''}`}
      onClick={() => onSelect(value)}
    >
      <span className="name">{label}</span>
      <span className="desc">{description}</span>
    </button>
  );
}

function ConfirmDialog({ onCancel, onConfirm }) {
  return (
    <div className="lat-alert crit" role="alertdialog">
      <p>
        This device looks like it&apos;s already configured. Continuing will
        reset its wireless, network, firewall, DHCP, and batman state.
      </p>
      <div className="setup-nav">
        <button type="button" className="lat-btn ghost" onClick={onCancel}>Cancel</button>
        <button type="button" className="lat-btn danger solid" onClick={onConfirm}>Reset and continue</button>
      </div>
    </div>
  );
}
