// =============================================================================
// StepUplink.jsx — Uplink configuration (Mesh Gate only)
// =============================================================================
//
// Visible only when role=MESH_GATE. Lets the operator pick how the
// gateway reaches the upstream network: a wired ethernet port or an
// existing Wi-Fi network (STA mode). "None" is intentionally absent —
// a gateway requires an uplink (the plan's design decision); operators
// who want a mesh-only node pick MESH_POINT on Step 1 instead.

import { useEffect, useMemo } from 'react';
import LatSelect from '../../components/LatSelect.jsx';
import { useSetup, SETUP_ACTIONS } from '../../contexts/SetupContext.jsx';
import { UplinkType } from '../../gen/openmanet/setup/v1/setup_pb.js';
import { WifiEncryption } from '../../gen/openmanet/wifi_config/v1/wifi_config_pb.js';
import { ENCRYPTION_LABELS, UPLINK_TYPE_LABELS } from './labels.js';

// STA encryption choices — the wizard supports the WPA family plus open.
const STA_ENCRYPTION_VALUES = [
  WifiEncryption.SAE,
  WifiEncryption.PSK2,
  WifiEncryption.PSK_MIXED,
  WifiEncryption.PSK,
  WifiEncryption.OWE,
  WifiEncryption.NONE,
];

export default function StepUplink({ status }) {
  const { state, dispatch } = useSetup();

  const ethernetPorts = useMemo(
    () => status?.ethernetPorts ?? [],
    [status],
  );
  // Non-HaLow radios are eligible STA radios. Wireless STA on the
  // mesh radio itself is rejected by handler validation.
  const staRadios = useMemo(
    () => (status?.radios ?? []).filter(r => !r.isHalow),
    [status],
  );

  // Default uplink type if none chosen yet.
  useEffect(() => {
    if (state.uplink.type === UplinkType.UNSPECIFIED) {
      // Prefer ethernet if any port is available, else WIRELESS_STA.
      const t = ethernetPorts.length > 0 ? UplinkType.ETHERNET : UplinkType.WIRELESS_STA;
      dispatch({ type: SETUP_ACTIONS.SET_UPLINK_TYPE, value: t });
    }
  }, [state.uplink.type, ethernetPorts.length, dispatch]);

  // Default ethernet port if user picked Ethernet but none chosen.
  useEffect(() => {
    if (
      state.uplink.type === UplinkType.ETHERNET &&
      !state.uplink.ethernetPort &&
      ethernetPorts.length > 0
    ) {
      dispatch({ type: SETUP_ACTIONS.SET_UPLINK_FIELD, field: 'ethernetPort', value: ethernetPorts[0] });
    }
  }, [state.uplink.type, state.uplink.ethernetPort, ethernetPorts, dispatch]);

  // Default STA radio if user picked Wireless but none chosen.
  useEffect(() => {
    if (
      state.uplink.type === UplinkType.WIRELESS_STA &&
      !state.uplink.wireless.radioName &&
      staRadios.length > 0
    ) {
      dispatch({
        type: SETUP_ACTIONS.SET_UPLINK_WIRELESS,
        field: 'radioName', value: staRadios[0].name,
      });
    }
  }, [state.uplink.type, state.uplink.wireless.radioName, staRadios, dispatch]);

  return (
    <div className="setup-step">
      <h3>Uplink</h3>
      <p className="setup-help">
        How does this gateway reach the upstream network?
      </p>

      <div className="lat-field">
        <label>Uplink type</label>
        <LatSelect
          ariaLabel="Uplink type"
          value={state.uplink.type}
          options={[
            { value: UplinkType.ETHERNET, label: UPLINK_TYPE_LABELS[UplinkType.ETHERNET] },
            { value: UplinkType.WIRELESS_STA, label: UPLINK_TYPE_LABELS[UplinkType.WIRELESS_STA] },
          ]}
          onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_UPLINK_TYPE, value: v })}
        />
      </div>

      {state.uplink.type === UplinkType.ETHERNET && (
        <div className="lat-field">
          <label>Ethernet port</label>
          {ethernetPorts.length === 0 ? (
            <div className="lat-alert warn">No ethernet ports detected.</div>
          ) : (
            <LatSelect
              ariaLabel="Ethernet port"
              value={state.uplink.ethernetPort}
              options={ethernetPorts.map(p => ({ value: p, label: p }))}
              onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_UPLINK_FIELD, field: 'ethernetPort', value: v })}
            />
          )}
        </div>
      )}

      {state.uplink.type === UplinkType.WIRELESS_STA && (
        <>
          <div className="lat-field">
            <label>STA radio</label>
            {staRadios.length === 0 ? (
              <div className="lat-alert warn">No non-HaLow radios available for an STA uplink.</div>
            ) : (
              <LatSelect
                ariaLabel="STA radio"
                value={state.uplink.wireless.radioName}
                options={staRadios.map(r => ({ value: r.name, label: r.name }))}
                onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_UPLINK_WIRELESS, field: 'radioName', value: v })}
              />
            )}
          </div>

          <div className="lat-field">
            <label htmlFor="setup-sta-ssid">Upstream SSID</label>
            <input
              id="setup-sta-ssid"
              className="lat-input"
              type="text"
              value={state.uplink.wireless.ssid}
              onChange={(e) => dispatch({ type: SETUP_ACTIONS.SET_UPLINK_WIRELESS, field: 'ssid', value: e.target.value })}
              maxLength={32}
              autoComplete="off"
              spellCheck={false}
            />
          </div>

          <div className="lat-field">
            <label>Encryption</label>
            <LatSelect
              ariaLabel="STA encryption"
              value={state.uplink.wireless.encryption}
              options={STA_ENCRYPTION_VALUES.map(v => ({ value: v, label: ENCRYPTION_LABELS[v] }))}
              onChange={(v) => dispatch({ type: SETUP_ACTIONS.SET_UPLINK_WIRELESS, field: 'encryption', value: v })}
            />
          </div>

          {state.uplink.wireless.encryption !== WifiEncryption.NONE &&
           state.uplink.wireless.encryption !== WifiEncryption.OWE && (
            <div className="lat-field">
              <label htmlFor="setup-sta-pass">Upstream passphrase</label>
              <input
                id="setup-sta-pass"
                className="lat-input"
                type="password"
                value={state.uplink.wireless.passphrase}
                onChange={(e) => dispatch({ type: SETUP_ACTIONS.SET_UPLINK_WIRELESS, field: 'passphrase', value: e.target.value })}
                minLength={8}
                maxLength={63}
                autoComplete="new-password"
              />
            </div>
          )}
        </>
      )}
    </div>
  );
}
