// =============================================================================
// SetupBeforeUnloadGuard.jsx — Confirms before leaving the wizard mid-flow
// =============================================================================
//
// The wizard's state lives in client memory and a refresh restarts the
// flow. To avoid silently losing 5+ minutes of input on an accidental
// F5 / tab-close, we attach a beforeunload listener whenever the user
// has typed anything and the wizard hasn't already succeeded.

import { useEffect, useRef } from 'react';
import { useSetup } from '../../contexts/SetupContext.jsx';
import { initialState } from '../../contexts/SetupContext.jsx';

// hasMeaningfulInput returns true if any user-typed field differs from
// the initial state. Excludes the role default (Mesh Point) so a user
// who lands on the wizard but has typed nothing isn't prompted.
function hasMeaningfulInput(state) {
  if (state.hostname) return true;
  if (state.adminPassword || state.adminPasswordConfirm) return true;
  if (state.mesh.passphrase) return true;
  if (state.mesh.meshId !== initialState.mesh.meshId) return true;
  if (state.mesh.radioName) return true;
  if (state.uplink.ethernetPort) return true;
  if (state.uplink.wireless.ssid) return true;
  if (state.uplink.wireless.passphrase) return true;
  if (state.aps.length > 0) return true;
  return false;
}

export default function SetupBeforeUnloadGuard() {
  const { state } = useSetup();
  // succeededRef flips to true after StepReview reports a successful
  // apply, so the wizard's own success-page navigation doesn't trigger
  // a confirmation prompt.
  const succeededRef = useRef(false);

  useEffect(() => {
    const onApplied = () => { succeededRef.current = true; };
    window.addEventListener('setup-applied', onApplied);
    return () => window.removeEventListener('setup-applied', onApplied);
  }, []);

  useEffect(() => {
    const handler = (e) => {
      if (succeededRef.current) return;
      if (!hasMeaningfulInput(state)) return;
      e.preventDefault();
      // Modern browsers ignore the returnValue text, but it must be set
      // to a truthy string for the prompt to fire on some browsers.
      e.returnValue = 'You have unsaved changes in the setup wizard.';
      return e.returnValue;
    };

    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [state]);

  return null;
}
