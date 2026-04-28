// =============================================================================
// StepPassword.jsx — Admin password (set during apply, locks the wizard
// =============================================================================
//
// Once apply succeeds, auth.enable flips on and this password becomes
// the only way to manage the device. The minimum length matches the
// handler's phase 1 validation (8 characters).

import { useSetup, SETUP_ACTIONS } from '../../contexts/SetupContext.jsx';

export default function StepPassword() {
  const { state, dispatch } = useSetup();

  const tooShort = state.adminPassword && state.adminPassword.length < 8;
  const mismatch = state.adminPasswordConfirm &&
                   state.adminPassword !== state.adminPasswordConfirm;

  return (
    <div className="setup-step">
      <h3>Admin Password</h3>
      <p className="setup-help">
        After setup completes, this password protects access to the
        OpenMANET web UI. Choose something you&apos;ll remember — there&apos;s no
        recovery flow, only a factory reset.
      </p>

      <div className="lat-field">
        <label htmlFor="setup-admin-pass">Password</label>
        <input
          id="setup-admin-pass"
          className="lat-input"
          type="password"
          value={state.adminPassword}
          onChange={(e) => dispatch({ type: SETUP_ACTIONS.SET_ADMIN_PASSWORD, value: e.target.value })}
          autoComplete="new-password"
          minLength={8}
        />
        {tooShort && (
          <div className="setup-error">Password must be at least 8 characters.</div>
        )}
      </div>

      <div className="lat-field">
        <label htmlFor="setup-admin-pass-confirm">Confirm password</label>
        <input
          id="setup-admin-pass-confirm"
          className="lat-input"
          type="password"
          value={state.adminPasswordConfirm}
          onChange={(e) => dispatch({ type: SETUP_ACTIONS.SET_ADMIN_PASSWORD_CONFIRM, value: e.target.value })}
          autoComplete="new-password"
        />
        {mismatch && (
          <div className="setup-error">Passwords don&apos;t match.</div>
        )}
      </div>
    </div>
  );
}
