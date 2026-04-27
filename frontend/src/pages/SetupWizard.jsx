// =============================================================================
// SetupWizard.jsx — First-boot mesh-node setup flow
// =============================================================================
//
// 6-step wizard: Identity & Role / Mesh / Uplink / APs / Password / Review.
// Mesh Point users skip the Uplink step automatically (5 steps in practice).
// State lives in <SetupProvider>; this component only owns currentIndex
// and the GetSetupStatus payload (so step components can render radio +
// ethernet-port lists without re-fetching).
//
// On apply (StepReview), a streaming ApplySetup runs through all 14
// phases. The terminal event is success/failure; if the stream drops
// before the terminal arrives, StepReview falls back to polling
// GetSetupStatus.

import { useEffect, useMemo, useState } from 'react';
import { Empty } from '@bufbuild/protobuf';
import { setupClient } from '../services/setupClient.js';
import {
  SetupProvider,
  useSetup,
} from '../contexts/SetupContext.jsx';
import { MeshRole } from '../gen/openmanet/setup/v1/setup_pb.js';
import Stepper from '../components/Stepper.jsx';
import StepIdentity from './setup/StepIdentity.jsx';
import StepMesh from './setup/StepMesh.jsx';
import StepUplink from './setup/StepUplink.jsx';
import StepAPs from './setup/StepAPs.jsx';
import StepPassword from './setup/StepPassword.jsx';
import StepReview from './setup/StepReview.jsx';
import SetupBeforeUnloadGuard from './setup/SetupBeforeUnloadGuard.jsx';
import './SetupWizard.css';

const STEP_DEFS = [
  { key: 'identity', label: 'Identity', component: StepIdentity, role: 'all'   },
  { key: 'mesh',     label: 'Mesh',     component: StepMesh,     role: 'all'   },
  { key: 'uplink',   label: 'Uplink',   component: StepUplink,   role: 'gate'  },
  { key: 'aps',      label: 'APs',      component: StepAPs,      role: 'all'   },
  { key: 'password', label: 'Password', component: StepPassword, role: 'all'   },
  { key: 'review',   label: 'Review',   component: StepReview,   role: 'all'   },
];

export default function SetupWizard() {
  return (
    <SetupProvider>
      <SetupBeforeUnloadGuard />
      <SetupWizardShell />
    </SetupProvider>
  );
}

function SetupWizardShell() {
  const { state, dispatch } = useSetup();
  const [currentIndex, setCurrentIndex] = useState(0);
  const [status, setStatus] = useState(null);
  const [statusError, setStatusError] = useState(null);

  // Load the status payload on mount so step components have radio +
  // ethernet-port data available without re-fetching.
  useEffect(() => {
    let cancelled = false;
    setupClient.getSetupStatus(new Empty())
      .then((resp) => {
        if (cancelled) return;
        setStatus(resp);
        // Pre-fill the mesh radio with the first HaLow radio so the
        // user doesn't have to pick when there's an obvious choice.
        dispatch({ type: 'HYDRATE_FROM_STATUS', status: resp });
      })
      .catch((err) => { if (!cancelled) setStatusError(err); });
    return () => { cancelled = true; };
  }, [dispatch]);

  // Filter steps by role: Mesh Point hides the Uplink step.
  const steps = useMemo(() => {
    const role = state.role;
    return STEP_DEFS.filter(s => {
      if (s.role === 'all') return true;
      if (s.role === 'gate') return role === MeshRole.MESH_GATE;
      return false;
    });
  }, [state.role]);

  // Clamp currentIndex if the role flip removed the active step.
  useEffect(() => {
    if (currentIndex >= steps.length) setCurrentIndex(steps.length - 1);
  }, [currentIndex, steps.length]);

  if (statusError) {
    return (
      <div className="lat-body grid-3">
        <div className="lat-panel col-span-all">
          <div className="lat-alert crit">
            Failed to contact the setup service: {String(statusError.message || statusError)}
          </div>
        </div>
      </div>
    );
  }

  if (!status) {
    return <div className="lat-panel">Loading…</div>;
  }

  const step = steps[currentIndex];
  const StepBody = step.component;

  const goPrev = () => setCurrentIndex(i => Math.max(0, i - 1));
  const goNext = () => setCurrentIndex(i => Math.min(steps.length - 1, i + 1));

  const isFirst = currentIndex === 0;
  const isLast  = currentIndex === steps.length - 1;

  const labels = steps.map(s => s.label);

  return (
    <div className="setup-wizard">
      <div className="lat-view-header">
        <div>
          <h2>OpenMANET Setup Wizard</h2>
          <div className="crumb">Step {currentIndex + 1} of {steps.length}: {step.label}</div>
        </div>
      </div>

      <Stepper steps={labels} currentIndex={currentIndex} />

      <div className="lat-body grid-3">
        <div className="lat-panel col-span-all">
          <StepBody status={status} onAdvance={isLast ? undefined : goNext} />
        </div>
      </div>

      {!isLast && (
        <div className="setup-nav">
          <button
            type="button"
            className="lat-btn ghost"
            onClick={goPrev}
            disabled={isFirst}
          >
            Back
          </button>
          <button
            type="button"
            className="lat-btn primary"
            onClick={goNext}
          >
            Next
          </button>
        </div>
      )}
    </div>
  );
}
