// =============================================================================
// MicMeter.jsx — Microphone level meter bar
// =============================================================================
// Horizontal bar with green-yellow-red gradient showing mic input level.
// Uses requestAnimationFrame for smooth display with fast attack / slow release.
//
// Props:
//   level  — 0-1 RMS value from mic input
//   active — boolean, whether PTT is active (resets meter when inactive)

import React, { useRef, useEffect, useState } from 'react';

export default function MicMeter({ level, active, voxEnabled, segments = 0 }) {
  const smoothRef = useRef(0);
  const animRef = useRef(null);
  const [displayPct, setDisplayPct] = useState(0);
  const [displayDb, setDisplayDb] = useState('');

  // Keep props in refs so the animation loop always reads the latest values
  // without restarting the rAF loop on every update.
  const levelRef = useRef(level);
  const activeRef = useRef(active);
  useEffect(() => {
    levelRef.current = level;
    activeRef.current = active;
  });

  const showMeter = active || voxEnabled;

  useEffect(() => {
    if (!showMeter) {
      smoothRef.current = 0;
      return undefined;
    }

    function update() {
      let smooth = smoothRef.current;
      const currentLevel = levelRef.current;

      // Fast attack, slow release smoothing.
      if (currentLevel > smooth) {
        smooth = currentLevel;
      } else {
        smooth = smooth * 0.85 + currentLevel * 0.15;
      }

      smoothRef.current = smooth;

      const pct = Math.min(100, smooth * 300);
      const db = smooth > 0.0001 ? (20 * Math.log10(smooth)).toFixed(0) + ' dB' : '';

      setDisplayPct(pct);
      setDisplayDb(db);

      animRef.current = requestAnimationFrame(update);
    }

    animRef.current = requestAnimationFrame(update);
    return () => {
      if (animRef.current) cancelAnimationFrame(animRef.current);
    };
  }, [showMeter]);

  // When the meter is hidden, display zero/blank without an effect cycle.
  const visiblePct = showMeter ? displayPct : 0;
  const visibleDb = showMeter ? displayDb : '';

  if (segments > 0) {
    const lit = Math.round((visiblePct / 100) * segments);
    const segs = [];
    for (let i = 0; i < segments; i++) {
      let tier = 'ok';
      if (i >= segments - 2) tier = 'crit';
      else if (i >= Math.floor(segments * 0.6)) tier = 'warn';
      segs.push(
        <span
          key={i}
          className={`mic-seg mic-seg-${tier}${i < lit ? ' on' : ''}`}
        />
      );
    }
    return (
      <div className="mic-meter-segments">
        <div className="mic-meter-label">
          <span>MIC</span>
          <span className="mic-meter-db">{visibleDb || '—'}</span>
        </div>
        <div className="mic-meter-bar">{segs}</div>
      </div>
    );
  }

  return (
    <div className="viz-row">
      <span className="viz-label">MIC</span>
      <div className="meter-wrap">
        <div className="meter-bar" style={{ width: visiblePct + '%' }} />
        <div className="meter-db">{visibleDb}</div>
      </div>
    </div>
  );
}
