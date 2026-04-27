// =============================================================================
// StepReview.jsx — Final review + ApplySetup streaming RPC
// =============================================================================
//
// Renders a summary of every field the user entered, then on click
// kicks off the streaming ApplySetup RPC. The wizard tracks per-phase
// status as events arrive and renders three terminal cases:
//
//   stream completes with PHASE_TERMINAL success=true   → success page
//   stream completes with PHASE_TERMINAL success=false  → failure page
//   stream ends abruptly (network reload severed it)    → ambiguous page,
//                                                          poll GetSetupStatus
//                                                          for 60s

import { useEffect, useRef, useState } from 'react';
import { Empty } from '@bufbuild/protobuf';
import { setupClient } from '../../services/setupClient.js';
import { useSetup } from '../../contexts/SetupContext.jsx';
import {
  ApplySetupRequest,
  ApplySetupResponse_Phase as Phase,
  ApplySetupResponse_Status as Status,
  MeshNodeProfile,
  MeshRadioConfig,
  MeshRole,
  Uplink,
  RadioApProfile,
  WifiStaProfile,
  UplinkType,
} from '../../gen/openmanet/setup/v1/setup_pb.js';
import {
  ROLE_LABELS,
  MESH_POINT_MODE_LABELS,
  MESH_GATE_MODE_LABELS,
  UPLINK_TYPE_LABELS,
  ENCRYPTION_LABELS,
} from './labels.js';

// Phase metadata: each tuple is [enum, label] in the canonical order
// the events arrive. Phase 99 (TERMINAL) is excluded from the list and
// rendered separately in the terminal panel.
const PHASE_DEFS = [
  [Phase.VALIDATE,           'Validate'],
  [Phase.SNAPSHOT,           'Snapshot'],
  [Phase.RESET_WIRELESS,     'Reset wireless'],
  [Phase.RESET_NETWORK,      'Reset network'],
  [Phase.HOSTNAME,           'Hostname'],
  [Phase.BASE_NETWORK,       'Base network'],
  [Phase.WIRELESS_MESH,      'Mesh'],
  [Phase.PER_RADIO_AP_STA,   'Per-radio'],
  [Phase.SCENARIO_TOPOLOGY,  'Topology'],
  [Phase.BATMAN_ADV,         'Batman-adv'],
  [Phase.MESH11SD,           'mesh11sd'],
  [Phase.COMMIT,             'Commit'],
  [Phase.PASSWORD,           'Password'],
  [Phase.PERSIST_FLAGS,      'Persist flags'],
];

const POLL_INTERVAL_MS = 4000;
const POLL_TIMEOUT_MS  = 60000;

export default function StepReview() {
  const { state } = useSetup();
  const [phaseMap, setPhaseMap] = useState({});  // phase enum → 'started'|'done'|'failed'
  const [phaseError, setPhaseError] = useState(null); // {phase, message}
  const [terminal, setTerminal] = useState(null); // 'success' | 'failure' | 'ambiguous'
  const [terminalResult, setTerminalResult] = useState(null);
  const [busy, setBusy] = useState(false);
  const cancelRef = useRef(false);

  useEffect(() => () => { cancelRef.current = true; }, []);

  const apply = async () => {
    setBusy(true);
    setPhaseMap({});
    setPhaseError(null);
    setTerminal(null);

    const req = new ApplySetupRequest({ profile: profileToProto(state) });
    let sawTerminal = false;

    try {
      const stream = setupClient.applySetup(req);

      for await (const ev of stream) {
        if (cancelRef.current) break;

        if (ev.phase === Phase.TERMINAL) {
          sawTerminal = true;
          setTerminalResult(ev.result ?? null);
          setTerminal(ev.result?.success ? 'success' : 'failure');
          // Notify the BeforeUnload guard so the success-page
          // navigation doesn't trigger a confirmation prompt.
          window.dispatchEvent(new Event('setup-applied'));
          break;
        }

        const status = statusKey(ev.status);
        if (status === 'failed') {
          setPhaseError({ phase: ev.phase, message: ev.message });
        }
        setPhaseMap(prev => ({ ...prev, [ev.phase]: status }));
      }
    } catch (err) {
      if (cancelRef.current) return;

      // Stream ended abruptly. Either the apply succeeded and the
      // network reload severed our connection, or it really failed.
      // Poll GetSetupStatus to disambiguate.
      if (!sawTerminal) {
        setTerminal('ambiguous');
        await pollForCompletion({
          onSuccess: () => {
            if (cancelRef.current) return;
            setTerminal('success');
            window.dispatchEvent(new Event('setup-applied'));
          },
          onTimeout: () => {
            if (cancelRef.current) return;
            // Stay on the ambiguous page — show the user the expected
            // SSID/URL and let them try the new connection.
          },
        });
      } else if (!terminal) {
        setPhaseError({ phase: Phase.UNSPECIFIED, message: String(err.message || err) });
        setTerminal('failure');
      }
    } finally {
      setBusy(false);
    }
  };

  if (terminal === 'success') {
    return <SuccessPanel state={state} result={terminalResult} />;
  }

  if (terminal === 'failure') {
    return <FailurePanel error={phaseError} onRetry={() => { setTerminal(null); }} />;
  }

  if (terminal === 'ambiguous') {
    return <AmbiguousPanel state={state} />;
  }

  return (
    <div className="setup-step">
      <h3>Review &amp; Apply</h3>

      <div className="lat-alert crit">
        Applying these settings will reload network services. The device
        will likely move to a new IP address and SSID; you&apos;ll need to
        reconnect.
      </div>

      <ReviewSummary state={state} />

      <div className="setup-nav">
        <button
          type="button"
          className="lat-btn primary"
          disabled={busy || !canApply(state)}
          onClick={apply}
        >
          {busy ? 'Applying…' : 'Apply'}
        </button>
      </div>

      {busy && (
        <ul className="setup-apply-phases" aria-label="Apply progress">
          {PHASE_DEFS.map(([phase, label]) => {
            const st = phaseMap[phase];
            return (
              <li key={phase}>
                <span className={`phase-status ${st ?? ''}`}>
                  {st ?? '—'}
                </span>
                <span>{label}</span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function statusKey(status) {
  switch (status) {
    case Status.STARTED: return 'started';
    case Status.DONE:    return 'done';
    case Status.FAILED:  return 'failed';
    default:             return undefined;
  }
}

function canApply(state) {
  if (!state.hostname) return false;
  if (!state.adminPassword || state.adminPassword.length < 8) return false;
  if (state.adminPassword !== state.adminPasswordConfirm) return false;
  if (!state.mesh.radioName || !state.mesh.meshId) return false;
  return true;
}

// profileToProto converts the wizard's reducer state into a
// MeshNodeProfile message for ApplySetup. Field names match the proto
// `name` field, NOT the JS localName.
function profileToProto(state) {
  const profile = new MeshNodeProfile({
    hostname:      state.hostname,
    adminPassword: state.adminPassword,
    role:          state.role,
    mesh: new MeshRadioConfig({
      radioName:    state.mesh.radioName,
      meshId:       state.mesh.meshId,
      passphrase:   state.mesh.passphrase,
      encryption:   state.mesh.encryption,
      bandwidthMhz: state.mesh.bandwidthMhz,
      channel:      state.mesh.channel,
    }),
    aps: state.aps.filter(a => a.enabled).map(a => new RadioApProfile({
      radioName:  a.radioName,
      enabled:    true,
      ssid:       a.ssid,
      passphrase: a.passphrase,
      encryption: a.encryption,
    })),
  });

  if (state.role === MeshRole.MESH_POINT) {
    profile.deviceMode = { case: 'meshpointMode', value: state.meshpointMode };
  } else if (state.role === MeshRole.MESH_GATE) {
    profile.deviceMode = { case: 'meshgateMode',  value: state.meshgateMode  };
    profile.uplink = new Uplink({
      type:         state.uplink.type,
      ethernetPort: state.uplink.ethernetPort,
      wireless: state.uplink.type === UplinkType.WIRELESS_STA
        ? new WifiStaProfile({
            radioName:  state.uplink.wireless.radioName,
            ssid:       state.uplink.wireless.ssid,
            passphrase: state.uplink.wireless.passphrase,
            encryption: state.uplink.wireless.encryption,
          })
        : undefined,
    });
  }

  return profile;
}

async function pollForCompletion({ onSuccess, onTimeout }) {
  const deadline = Date.now() + POLL_TIMEOUT_MS;
  while (Date.now() < deadline) {
    try {
      const resp = await setupClient.getSetupStatus(new Empty());
      if (resp.isSetupComplete) { onSuccess(); return; }
    } catch {
      /* network errors are expected during reload; keep polling */
    }
    await new Promise(r => setTimeout(r, POLL_INTERVAL_MS));
  }
  onTimeout();
}

function ReviewSummary({ state }) {
  const apEntries = state.aps.filter(a => a.enabled);
  return (
    <>
      <div className="kv"><span className="k">Hostname</span><span className="v">{state.hostname}</span></div>
      <div className="kv"><span className="k">Role</span><span className="v">{ROLE_LABELS[state.role] ?? '?'}</span></div>
      {state.role === MeshRole.MESH_POINT && (
        <div className="kv">
          <span className="k">Mesh point mode</span>
          <span className="v">{MESH_POINT_MODE_LABELS[state.meshpointMode] ?? '?'}</span>
        </div>
      )}
      {state.role === MeshRole.MESH_GATE && (
        <>
          <div className="kv">
            <span className="k">Gate mode</span>
            <span className="v">{MESH_GATE_MODE_LABELS[state.meshgateMode] ?? '?'}</span>
          </div>
          <div className="kv">
            <span className="k">Uplink</span>
            <span className="v">{UPLINK_TYPE_LABELS[state.uplink.type] ?? '?'}</span>
          </div>
        </>
      )}
      <div className="kv"><span className="k">Mesh radio</span><span className="v">{state.mesh.radioName}</span></div>
      <div className="kv"><span className="k">Mesh ID</span><span className="v">{state.mesh.meshId}</span></div>
      <div className="kv">
        <span className="k">Channel / bandwidth</span>
        <span className="v">{state.mesh.channel} @ {state.mesh.bandwidthMhz} MHz</span>
      </div>
      <div className="kv">
        <span className="k">Mesh encryption</span>
        <span className="v">{ENCRYPTION_LABELS[state.mesh.encryption] ?? '?'}</span>
      </div>
      <div className="kv">
        <span className="k">Client APs</span>
        <span className="v">
          {apEntries.length === 0
            ? 'none'
            : apEntries.map(a => `${a.radioName}: ${a.ssid}`).join(', ')}
        </span>
      </div>
    </>
  );
}

function SuccessPanel({ state, result }) {
  const url = result?.expectedUrl || `https://${state.hostname}.local:8081/login`;
  const ssid = result?.expectedSsid;
  return (
    <div className="setup-step" role="status">
      <h3>Setup Complete</h3>
      <div className="lat-alert ok">Your device is configured.</div>
      <p>To continue managing this device:</p>
      <ol>
        {ssid && <li>Connect to the new mesh AP: <strong>{ssid}</strong></li>}
        <li>Open <a href={url}>{url}</a></li>
        <li>Sign in with the admin password you just set.</li>
      </ol>
      <p className="setup-help">
        If this is the only device on the mesh, your management IP will
        remain at the value chosen during setup; the address will
        renumber automatically when other peers join.
      </p>
    </div>
  );
}

function FailurePanel({ error, onRetry }) {
  return (
    <div className="setup-step" role="alert">
      <h3>Setup Failed</h3>
      <div className="lat-alert crit">
        {error?.message ?? 'The wizard could not complete.'}
        {error?.phase !== undefined && (
          <div className="setup-help" style={{ marginTop: 4 }}>
            Failed phase: {Phase[error.phase] ?? error.phase}
          </div>
        )}
      </div>
      <div className="setup-nav">
        <button type="button" className="lat-btn primary" onClick={onRetry}>Back to review</button>
      </div>
    </div>
  );
}

function AmbiguousPanel({ state }) {
  const url = `https://${state.hostname}.local:8081/login`;
  return (
    <div className="setup-step" role="status">
      <h3>Confirming…</h3>
      <p>
        The wizard&apos;s connection to the device dropped during setup —
        this is expected when the network reconfigures. We&apos;re checking
        whether setup completed.
      </p>
      <p className="setup-help">
        If this takes more than a minute, try connecting now: <a href={url}>{url}</a>
      </p>
    </div>
  );
}
