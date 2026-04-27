// =============================================================================
// StepAPs.jsx — Per-radio Wi-Fi access point configuration
// =============================================================================
//
// One toggle + form per non-mesh radio. Disabled by default — the user
// opts in for each radio they want broadcasting an AP. The "client
// device Wi-Fi" framing comes from the plan: this is where end users'
// phones / laptops join the mesh through.

import LatSelect from '../../components/LatSelect.jsx';
import { useSetup, SETUP_ACTIONS } from '../../contexts/SetupContext.jsx';
import { WifiEncryption } from '../../gen/openmanet/wifi_config/v1/wifi_config_pb.js';
import { ENCRYPTION_LABELS } from './labels.js';

const AP_ENCRYPTION_VALUES = [
  WifiEncryption.SAE,
  WifiEncryption.PSK2,
  WifiEncryption.PSK_MIXED,
  WifiEncryption.OWE,
  WifiEncryption.NONE,
];

export default function StepAPs({ status }) {
  const { state, dispatch } = useSetup();

  const apRadios = (status?.radios ?? []).filter(r => !r.isHalow);

  if (apRadios.length === 0) {
    return (
      <div className="setup-step">
        <h3>Client Device Wi-Fi</h3>
        <p>
          This device has no non-HaLow radios available for client APs.
          You can still continue without configuring any.
        </p>
      </div>
    );
  }

  return (
    <div className="setup-step">
      <h3>Client Device Wi-Fi</h3>
      <p className="setup-help">
        Optional: configure a Wi-Fi access point on each radio so phones
        and laptops can join your mesh.
      </p>

      {apRadios.map(radio => (
        <APRow key={radio.name} radio={radio} state={state} dispatch={dispatch} />
      ))}
    </div>
  );
}

function APRow({ radio, state, dispatch }) {
  const ap = state.aps.find(a => a.radioName === radio.name) ?? {
    radioName: radio.name,
    enabled:    false,
    ssid:       '',
    passphrase: '',
    encryption: WifiEncryption.PSK2,
  };

  const setField = (field, value) => {
    dispatch({
      type: SETUP_ACTIONS.SET_AP,
      value: { ...ap, [field]: value, radioName: radio.name },
    });
  };

  return (
    <div className="setup-ap-row">
      <div className="setup-ap-head">
        <div>
          <strong>{radio.name}</strong>
          {radio.band && <span className="setup-help" style={{ marginLeft: 8 }}>{radio.band}</span>}
        </div>
        <button
          type="button"
          className={`lat-toggle ${ap.enabled ? 'on' : ''}`}
          aria-checked={ap.enabled}
          role="switch"
          aria-label={`Enable AP on ${radio.name}`}
          onClick={() => setField('enabled', !ap.enabled)}
        >
          <span className="thumb" />
        </button>
      </div>

      {ap.enabled && (
        <>
          <div className="lat-field">
            <label htmlFor={`setup-ap-ssid-${radio.name}`}>SSID</label>
            <input
              id={`setup-ap-ssid-${radio.name}`}
              className="lat-input"
              type="text"
              value={ap.ssid}
              onChange={(e) => setField('ssid', e.target.value)}
              maxLength={32}
              autoComplete="off"
              spellCheck={false}
            />
          </div>

          <div className="lat-field">
            <label>Encryption</label>
            <LatSelect
              ariaLabel={`Encryption on ${radio.name}`}
              value={ap.encryption}
              options={AP_ENCRYPTION_VALUES.map(v => ({ value: v, label: ENCRYPTION_LABELS[v] }))}
              onChange={(v) => setField('encryption', v)}
            />
          </div>

          {ap.encryption !== WifiEncryption.NONE && ap.encryption !== WifiEncryption.OWE && (
            <div className="lat-field">
              <label htmlFor={`setup-ap-pass-${radio.name}`}>Passphrase</label>
              <input
                id={`setup-ap-pass-${radio.name}`}
                className="lat-input"
                type="password"
                value={ap.passphrase}
                onChange={(e) => setField('passphrase', e.target.value)}
                minLength={8}
                maxLength={63}
                autoComplete="new-password"
              />
              {ap.passphrase && ap.passphrase.length < 8 && (
                <div className="setup-error">Passphrase must be at least 8 characters.</div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
