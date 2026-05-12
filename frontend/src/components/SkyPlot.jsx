// =============================================================================
// SkyPlot.jsx — Polar view of GNSS satellites (elevation + azimuth)
// =============================================================================
// Renders each satellite at its (azimuth, elevation) on a zenith-centered
// polar chart.  Rings at 30° / 60° / horizon. Used sats = green, weak
// (SNR < 30) = amber, unused = cyan outline.

import React from 'react';

const SIZE = 220;
const MARGIN = 2;  // reserve pixels for rim stroke so dots at horizon aren't clipped
const HORIZON_R = SIZE / 2 - MARGIN;

// Convert azimuth/elevation to x,y on the polar canvas.
// azimuth 0° = north (up), increases clockwise.
// elevation 90° = zenith (center), 0° = horizon (rim).
function polarToXY(azimuthDeg, elevationDeg) {
  const azRad = (azimuthDeg * Math.PI) / 180;
  const rNorm = Math.max(0, Math.min(1, 1 - elevationDeg / 90));
  const r = rNorm * HORIZON_R;
  const cx = SIZE / 2;
  const cy = SIZE / 2;
  return {
    x: cx + r * Math.sin(azRad),
    y: cy - r * Math.cos(azRad),
  };
}

function satFillClass(sat) {
  if (sat.used) return 'skyplot-sat used';
  if ((sat.snr ?? 0) >= 30) return 'skyplot-sat';
  return 'skyplot-sat weak';
}

export default React.memo(function SkyPlot({ satellites = [], ariaLabel = 'Sky Plot' }) {
  const visible = satellites.filter(
    (s) => typeof s.azimuth === 'number' && typeof s.elevation === 'number'
  );

  return (
    <svg
      role="img"
      aria-label={ariaLabel}
      className="skyplot-svg"
      viewBox={`0 0 ${SIZE} ${SIZE}`}
      data-testid="skyplot"
    >
      {/* Horizon rim */}
      <circle cx={SIZE / 2} cy={SIZE / 2} r={HORIZON_R} className="skyplot-ring horizon" />
      {/* 30° elevation ring */}
      <circle cx={SIZE / 2} cy={SIZE / 2} r={HORIZON_R * (2 / 3)} className="skyplot-ring" />
      {/* 60° elevation ring */}
      <circle cx={SIZE / 2} cy={SIZE / 2} r={HORIZON_R * (1 / 3)} className="skyplot-ring" />
      {/* N/S/E/W crosshair */}
      <line x1={SIZE / 2} y1={MARGIN} x2={SIZE / 2} y2={SIZE - MARGIN} className="skyplot-cross" />
      <line x1={MARGIN} y1={SIZE / 2} x2={SIZE - MARGIN} y2={SIZE / 2} className="skyplot-cross" />
      {/* Cardinal labels */}
      <text x={SIZE / 2} y={MARGIN + 10} textAnchor="middle" className="skyplot-cardinal">N</text>
      <text x={SIZE - MARGIN - 6} y={SIZE / 2 + 4} textAnchor="end" className="skyplot-cardinal">E</text>
      <text x={SIZE / 2} y={SIZE - MARGIN - 4} textAnchor="middle" className="skyplot-cardinal">S</text>
      <text x={MARGIN + 6} y={SIZE / 2 + 4} textAnchor="start" className="skyplot-cardinal">W</text>

      {/* Satellites */}
      {visible.map((sat) => {
        const { x, y } = polarToXY(sat.azimuth, sat.elevation);
        return (
          <g key={sat.prn}>
            <circle cx={x} cy={y} r={7} className={satFillClass(sat)} />
            <text x={x} y={y + 3} textAnchor="middle" className="skyplot-sat-label">{sat.prn}</text>
          </g>
        );
      })}
    </svg>
  );
});
