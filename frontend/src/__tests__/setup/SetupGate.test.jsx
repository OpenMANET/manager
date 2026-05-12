// =============================================================================
// SetupGate.test.jsx — Branching logic for the wizard route gate
// =============================================================================
//
// Tests the pure helper `gateStateFromStatus` so we cover all four
// branches without standing up a full router. The integration of the
// gate with React Router is exercised by the manual E2E pass.

import { describe, it, expect } from 'vitest';
import { gateStateFromStatus } from '../../components/SetupGate.jsx';

describe('gateStateFromStatus', () => {
  it('returns "wizard-hidden" when setup is disabled', () => {
    expect(gateStateFromStatus({ isEnabled: false, isSetupComplete: false, hasHalowRadio: true }))
      .toBe('wizard-hidden');
  });

  it('returns "wizard-hidden" when setup is complete', () => {
    expect(gateStateFromStatus({ isEnabled: true, isSetupComplete: true, hasHalowRadio: true }))
      .toBe('wizard-hidden');
  });

  it('returns "no-halow" when enabled+incomplete but no HaLow radio', () => {
    expect(gateStateFromStatus({ isEnabled: true, isSetupComplete: false, hasHalowRadio: false }))
      .toBe('no-halow');
  });

  it('returns "wizard-active" when enabled+incomplete with HaLow radio', () => {
    expect(gateStateFromStatus({ isEnabled: true, isSetupComplete: false, hasHalowRadio: true }))
      .toBe('wizard-active');
  });

  it('returns "wizard-hidden" for an undefined status (fail-closed)', () => {
    expect(gateStateFromStatus(undefined)).toBe('wizard-hidden');
    expect(gateStateFromStatus(null)).toBe('wizard-hidden');
  });
});
