// =============================================================================
// StepAPs.test.jsx — AP encryption choices
// =============================================================================
//
// Pins the encryption options the wizard offers per AP radio. The set
// mirrors the LuCI mesh wizard (psk2 / sae-mixed / sae) plus the open
// modes; psk-mixed (WPA1+WPA2) is deliberately not offered because it
// was previously mislabelled as WPA2/WPA3.

import React from 'react';
import { describe, it, expect, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';

import StepAPs from '../../../pages/setup/StepAPs.jsx';
import { SetupProvider } from '../../../contexts/SetupContext.jsx';

const STATUS = {
  radios: [
    { name: 'radio0', band: '2g', isHalow: false },
    { name: 'radio1', band: 's1g', isHalow: true },
  ],
};

function renderStep() {
  return render(
    <SetupProvider>
      <StepAPs status={STATUS} />
    </SetupProvider>,
  );
}

afterEach(cleanup);

describe('StepAPsEncryptionOptions', () => {
  it('offers SAE, PSK2, SAE_MIXED, OWE and NONE in that order', () => {
    renderStep();

    fireEvent.click(screen.getByRole('switch', { name: 'Enable AP on radio0' }));
    fireEvent.click(screen.getByRole('button', { name: 'Encryption on radio0' }));

    const labels = screen.getAllByRole('option').map(o => o.textContent);
    expect(labels).toEqual([
      'WPA3 (SAE)',
      'WPA2 (PSK2)',
      'WPA2 / WPA3 (mixed)',
      'OWE (open, encrypted)',
      'None (open)',
    ]);
  });

  it('never offers the legacy WPA/WPA2 mixed mode', () => {
    renderStep();

    fireEvent.click(screen.getByRole('switch', { name: 'Enable AP on radio0' }));
    fireEvent.click(screen.getByRole('button', { name: 'Encryption on radio0' }));

    expect(screen.queryByRole('option', { name: 'WPA / WPA2 (mixed, legacy)' })).toBeNull();
  });
});
