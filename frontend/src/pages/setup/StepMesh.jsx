// =============================================================================
// StepMesh.jsx — Mesh radio configuration (mesh ID, passphrase, channel,
//                bandwidth) plus role-conditional sub-mode select
// =============================================================================
//
// The mesh-radio dropdown is filtered to HaLow radios only. When the
// device has exactly one HaLow radio (the common case), the field
// renders as a read-only display rather than a dropdown. Bandwidth is
// the parent control: changing it filters the channel list to legal
// values for that bandwidth (channel data comes from
// SetupRadio.bandwidths returned by GetSetupStatus).

import { useEffect, useMemo } from 'react';
import LatSelect from '../../components/LatSelect.jsx';
import { useSetup, SETUP_ACTIONS } from '../../contexts/SetupContext.jsx';
import {
  MeshRole,
  MeshPointMode,
} from '../../gen/openmanet/setup/v1/setup_pb.js';
import { WifiEncryption } from '../../gen/openmanet/wifi_config/v1/wifi_config_pb.js';
import {
  ENCRYPTION_LABELS,
  MESH_POINT_MODE_LABELS,
  MESH_GATE_MODE_LABELS,
  optionsFromMap,
} from './labels.js';

// Mesh encryption is restricted to SAE or NONE per the plan; PSK
// variants don't make sense on an 802.11s mesh.
const MESH_ENCRYPTION_VALUES = [
  WifiEncryption.SAE,
  WifiEncryption.NONE,
];

const BANDWIDTH_OPTIONS = [
  { value: 1, label: '1 MHz' },
  { value: 2, label: '2 MHz' },
  { value: 4, label: '4 MHz' },
  { value: 8, label: '8 MHz' },
];

// Default channel list per bandwidth when SetupRadio.bandwidths data
// isn't provided (e.g. in unit tests). Overridden by the radio's
// per-bandwidth list when available.
const FALLBACK_CHANNELS = {
  1: [1, 5, 9, 13, 17, 21, 25, 29, 33, 37, 41, 45, 49, 53, 57, 61, 65, 69],
  2: [3, 7, 11, 15, 19, 23, 27, 31, 35, 39, 43, 47, 51, 55, 59, 63, 67],
  4: [6, 14, 22, 30, 38, 46, 54, 62],
  8: [10, 26, 42, 58],
};

function bandwidthsForRadio(radio, bandwidthMhz) {
  const entry = (radio?.bandwidths ?? []).find(b => b.mhz === bandwidthMhz);
  if (entry?.channels?.length) return Array.from(entry.channels);
  return FALLBACK_CHANNELS[bandwidthMhz] ?? [];
}

export default function StepMesh({ status }) {
  const { state, dispatch } = useSetup();
  const halowRadios = useMemo(
    () => (status?.radios ?? []).filter(r => r.isHalow),
    [status],
  );

  // If the user hasn't picked a radio (HYDRATE_FROM_STATUS missed it
  // somehow), pick the first HaLow on render.
  useEffect(() => {
    if (!state.mesh.radioName && halowRadios.length > 0) {
      dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'radioName', value: halowRadios[0].name });
    }
  }, [state.mesh.radioName, halowRadios, dispatch]);

  const selectedRadio = halowRadios.find(r => r.name === state.mesh.radioName) ?? halowRadios[0];

  const channels = useMemo(
    () => bandwidthsForRadio(selectedRadio, state.mesh.bandwidthMhz),
    [selectedRadio, state.mesh.bandwidthMhz],
  );

  // Snap channel to the closest legal one when bandwidth changes.
  useEffect(() => {
    if (channels.length === 0) return;
    if (!channels.includes(state.mesh.channel)) {
      dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'channel', value: channels[0] });
    }
  }, [channels, state.mesh.channel, dispatch]);

  return (
    <div className="setup-step">
      <h3>Mesh Configuration</h3>

      {halowRadios.length === 0 && (
        <div className="lat-alert crit">
          No HaLow radio detected. The wizard cannot continue without
          one — see the previous screen for details.
        </div>
      )}

      {halowRadios.length === 1 ? (
        <div className="lat-field">
          <label>Mesh radio</label>
          <div className="lat-input" style={{ pointerEvents: 'none' }}>
            {halowRadios[0].name}
            {halowRadios[0].hardwareName && (
              <span className="setup-help" style={{ marginLeft: 8 }}>
                ({halowRadios[0].hardwareName})
              </span>
            )}
          </div>
        </div>
      ) : (
        <div className="lat-field">
          <label htmlFor="setup-mesh-radio">Mesh radio</label>
          <LatSelect
            ariaLabel="Mesh radio"
            value={state.mesh.radioName}
            options={halowRadios.map(r => ({ value: r.name, label: r.name }))}
            onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'radioName', value: v })}
          />
        </div>
      )}

      <div className="lat-field">
        <label htmlFor="setup-mesh-id">Mesh ID</label>
        <input
          id="setup-mesh-id"
          className="lat-input"
          type="text"
          value={state.mesh.meshId}
          onChange={(e) => dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'meshId', value: e.target.value })}
          maxLength={32}
          autoComplete="off"
          spellCheck={false}
        />
        <div className="setup-help">All nodes on the same mesh must share this ID.</div>
      </div>

      <div className="lat-field">
        <label>Encryption</label>
        <LatSelect
          ariaLabel="Mesh encryption"
          value={state.mesh.encryption}
          options={MESH_ENCRYPTION_VALUES.map(v => ({ value: v, label: ENCRYPTION_LABELS[v] }))}
          onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'encryption', value: v })}
        />
      </div>

      {state.mesh.encryption !== WifiEncryption.NONE && (
        <div className="lat-field">
          <label htmlFor="setup-mesh-pass">Mesh passphrase</label>
          <input
            id="setup-mesh-pass"
            className="lat-input"
            type="password"
            value={state.mesh.passphrase}
            onChange={(e) => dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'passphrase', value: e.target.value })}
            minLength={8}
            maxLength={63}
            autoComplete="new-password"
          />
          {state.mesh.passphrase && state.mesh.passphrase.length < 8 && (
            <div className="setup-error">Passphrase must be at least 8 characters.</div>
          )}
        </div>
      )}

      <div className="setup-field">
        <label>Bandwidth</label>
        <LatSelect
          ariaLabel="Mesh bandwidth"
          value={state.mesh.bandwidthMhz}
          options={BANDWIDTH_OPTIONS}
          onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'bandwidthMhz', value: v })}
        />
        <div className="setup-help">
          On 802.11ah, channel choice depends on bandwidth.
        </div>
      </div>

      <div className="setup-field">
        <label>Channel</label>
        <LatSelect
          ariaLabel="Mesh channel"
          value={state.mesh.channel}
          options={channels.map(c => ({ value: c, label: String(c) }))}
          onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'channel', value: v })}
        />
      </div>

      {state.role === MeshRole.MESH_GATE && (
        <div className="setup-field">
          <label>Gate mode</label>
          <LatSelect
            ariaLabel="Mesh gate mode"
            value={state.meshgateMode}
            options={optionsFromMap(MESH_GATE_MODE_LABELS)}
            onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_MESHGATE_MODE, value: v })}
          />
        </div>
      )}

      {state.role === MeshRole.MESH_POINT && (
        <div className="setup-field">
          <label>Mesh point mode</label>
          <LatSelect
            ariaLabel="Mesh point mode"
            value={state.meshpointMode}
            options={Object.entries(MESH_POINT_MODE_LABELS).map(([value, label]) => ({
              value: Number(value), label,
            })).filter(o => o.value !== MeshPointMode.UNSPECIFIED)}
            onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_MESHPOINT_MODE, value: v })}
          />
        </div>
      )}
    </div>
  );
}
