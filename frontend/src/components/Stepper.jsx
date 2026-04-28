// =============================================================================
// Stepper.jsx — Lattice-styled step indicator for multi-step flows
// =============================================================================
//
// Pure presentational component. Renders a row of numbered dots with the
// step labels below; the active step is glowing cyan, completed steps are
// muted with a check, future steps are dim.

import './Stepper.css';

export default function Stepper({ steps, currentIndex }) {
  return (
    <ol className="stepper" aria-label="Setup progress">
      {steps.map((label, i) => {
        let status = 'future';
        if (i < currentIndex) status = 'done';
        else if (i === currentIndex) status = 'active';

        return (
          <li
            key={label}
            className={`stepper-item stepper-${status}`}
            aria-current={status === 'active' ? 'step' : undefined}
          >
            <span className="stepper-dot">{status === 'done' ? '✓' : i + 1}</span>
            <span className="stepper-label">{label}</span>
          </li>
        );
      })}
    </ol>
  );
}
