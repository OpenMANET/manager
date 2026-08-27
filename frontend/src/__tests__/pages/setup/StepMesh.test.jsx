// =============================================================================
// StepMesh.test.jsx — mesh-point mode option list
// =============================================================================
//
// Pins wizard-parity ledger decision D1 (2026-08-27): MESH_POINT_MODE_NONE
// is not offered until openmanetd's address-reservation worker can leave
// a DHCP-client ahwlan alone. validateProfile rejects it on the backend;
// this test guards the UI half. The label stays in MESH_POINT_MODE_LABELS
// (labels.test.js requires every enum value to be labelled).

import React from 'react';
import { describe, it, expect, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';

import StepMesh from '../../../pages/setup/StepMesh.jsx';
import { SetupProvider } from '../../../contexts/SetupContext.jsx';
import { MESH_POINT_MODE_LABELS } from '../../../pages/setup/labels.js';
import { MeshPointMode } from '../../../gen/openmanet/setup/v1/setup_pb.js';

// initialState already selects MESH_POINT, so the mode select renders
// without any dispatch. One HaLow radio keeps the "no radio" alert away.
const STATUS = {
  radios: [{ name: 'radio1', band: 's1g', isHalow: true }],
  countries: [],
};

function renderStep() {
  return render(
    <SetupProvider>
      <StepMesh status={STATUS} />
    </SetupProvider>,
  );
}

afterEach(cleanup);

describe('StepMeshPointModeOptions', () => {
  it('offers Extender only; NONE stays hidden until the daemon supports it', () => {
    renderStep();

    fireEvent.click(screen.getByRole('button', { name: 'Mesh point mode' }));

    const labels = screen.getAllByRole('option').map(o => o.textContent);
    expect(labels).toEqual([MESH_POINT_MODE_LABELS[MeshPointMode.EXTENDER]]);
    expect(labels).not.toContain(MESH_POINT_MODE_LABELS[MeshPointMode.NONE]);
  });
});
