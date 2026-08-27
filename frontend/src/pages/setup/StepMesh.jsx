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

// Selectable mesh-point modes. MESH_POINT_MODE_NONE is hidden here (and
// rejected by validateProfile on the backend) until openmanetd's
// address-reservation worker can leave a DHCP-client ahwlan alone —
// wizard-parity ledger decision D1 (2026-08-27). The label stays in
// MESH_POINT_MODE_LABELS so the review summary can still name it.
const HIDDEN_MESH_POINT_MODES = new Set([MeshPointMode.UNSPECIFIED, MeshPointMode.NONE]);
const MESH_POINT_MODE_OPTIONS = optionsFromMap(MESH_POINT_MODE_LABELS)
  .filter(o => !HIDDEN_MESH_POINT_MODES.has(o.value));

const BANDWIDTH_LABELS = {
  1: '1 MHz',
  2: '2 MHz',
  4: '4 MHz',
  8: '8 MHz',
};

// Fallback US S1G channel allocations used only when the device's
// regulatory database (/usr/share/morse-regdb/channels.csv) was not
// loaded — e.g. on a developer machine without the Morse userspace
// package, or in unit tests that don't pass a fixture. Real devices
// always pull these per-country from GetSetupStatusResponse.countries.
//
// Reference: IEEE 802.11ah-2020 Annex E / Morse Micro firmware default
// regdom for US.
const FALLBACK_US_CHANNELS = {
  1: [1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23, 25, 27, 29, 31,
      33, 35, 37, 39, 41, 43, 45, 47, 49, 51],
  2: [2, 6, 10, 14, 18, 22, 26, 30, 34, 38, 42, 46, 50],
  4: [8, 16, 24, 32, 40, 48],
  8: [12, 28, 44],
};

// findCountryEntry returns the SetupCountry message for the given code,
// or undefined if not present.
function findCountryEntry(countries, code) {
  if (!countries || !code) return undefined;
  return countries.find(c => c.code === code);
}

// channelsForCountryBandwidth returns the legal channel list for the
// chosen (country, bandwidth) tuple, falling back to a baked-in US
// allocation when the regdb is absent.
function channelsForCountryBandwidth(countryEntry, bandwidthMhz) {
  if (countryEntry?.bandwidths) {
    const entry = countryEntry.bandwidths.find(b => b.mhz === bandwidthMhz);
    if (entry?.channels?.length) return Array.from(entry.channels);
  }
  return FALLBACK_US_CHANNELS[bandwidthMhz] ?? [];
}

// bandwidthsForCountry returns the bandwidths legal in this regulatory
// domain. Falls back to the four S1G widths when the regdb is empty.
function bandwidthsForCountry(countryEntry) {
  if (countryEntry?.bandwidths?.length) {
    return countryEntry.bandwidths.map(b => b.mhz).sort((a, b) => a - b);
  }
  return [1, 2, 4, 8];
}

export default function StepMesh({ status }) {
  const { state, dispatch } = useSetup();
  const halowRadios = useMemo(
    () => (status?.radios ?? []).filter(r => r.isHalow),
    [status],
  );

  const countries = useMemo(() => status?.countries ?? [], [status]);

  // If the user hasn't picked a radio (HYDRATE_FROM_STATUS missed it
  // somehow), pick the first HaLow on render.
  useEffect(() => {
    if (!state.mesh.radioName && halowRadios.length > 0) {
      dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'radioName', value: halowRadios[0].name });
    }
  }, [state.mesh.radioName, halowRadios, dispatch]);

  const countryEntry = useMemo(
    () => findCountryEntry(countries, state.mesh.countryCode),
    [countries, state.mesh.countryCode],
  );

  const legalBandwidths = useMemo(
    () => bandwidthsForCountry(countryEntry),
    [countryEntry],
  );

  // Snap bandwidth to a legal value when the country changes (e.g.
  // some EU countries only allow 1MHz / 2MHz HaLow).
  useEffect(() => {
    if (legalBandwidths.length === 0) return;
    if (!legalBandwidths.includes(state.mesh.bandwidthMhz)) {
      dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'bandwidthMhz', value: legalBandwidths[0] });
    }
  }, [legalBandwidths, state.mesh.bandwidthMhz, dispatch]);

  const channels = useMemo(
    () => channelsForCountryBandwidth(countryEntry, state.mesh.bandwidthMhz),
    [countryEntry, state.mesh.bandwidthMhz],
  );

  // Snap channel to the first legal one when (country, bandwidth) change.
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
        <div className="lat-input" style={{ pointerEvents: 'none' }}>
          {ENCRYPTION_LABELS[WifiEncryption.SAE]}
        </div>
        <div className="setup-help">
          Mesh links are always WPA3 (SAE). Open meshes are not supported.
        </div>
      </div>

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

      <div className="lat-field">
        <label>Country</label>
        {countries.length === 0 ? (
          <div className="lat-input" style={{ pointerEvents: 'none' }}>
            US (regulatory database not installed; falling back to baked-in defaults)
          </div>
        ) : (
          <LatSelect
            ariaLabel="Regulatory country"
            value={state.mesh.countryCode}
            options={countries.map(c => ({
              value: c.code,
              label: `${c.code} — ${c.name || c.code}`,
            }))}
            onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'countryCode', value: v })}
          />
        )}
        <div className="setup-help">
          Sets the regulatory domain on the HaLow radio. Determines which
          channels and bandwidths are legal here.
        </div>
      </div>

      <div className="setup-field">
        <label>Bandwidth</label>
        <LatSelect
          ariaLabel="Mesh bandwidth"
          value={state.mesh.bandwidthMhz}
          options={legalBandwidths.map(mhz => ({
            value: mhz, label: BANDWIDTH_LABELS[mhz] ?? `${mhz} MHz`,
          }))}
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
            options={MESH_POINT_MODE_OPTIONS}
            onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_MESHPOINT_MODE, value: v })}
          />
        </div>
      )}
    </div>
  );
}
