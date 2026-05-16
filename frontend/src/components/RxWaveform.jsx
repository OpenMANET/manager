// =============================================================================
// RxWaveform.jsx — Canvas-based RX audio waveform visualizer
// =============================================================================
// Draws scrolling vertical amplitude bars from the center line using
// requestAnimationFrame for smooth rendering.
//
// Props:
//   rxWaveDataRef     — ref to a Float32Array of peak amplitude history
//                       (parent owns the buffer; we read .current inside rAF
//                       so audio updates don't drive re-renders)
//   rxWaveWritePosRef — ref to the current write position (number)

import React, { useRef, useEffect } from 'react';

import { RX_WAVE_HISTORY } from '../constants.js';

export default function RxWaveform({
  rxWaveDataRef,
  rxWaveWritePosRef,
  inline,
  sourceTag = null,
  lattice = false,
}) {
  const canvasRef = useRef(null);
  const wrapRef = useRef(null);
  const animRef = useRef(null);
  const ctxRef = useRef(null);

  useEffect(() => {
    function draw() {
      const canvas = canvasRef.current;
      const wrap = wrapRef.current;
      if (!canvas || !wrap) {
        animRef.current = requestAnimationFrame(draw);
        return;
      }

      const W = wrap.clientWidth;
      const H = wrap.clientHeight;

      // Resize canvas to match container (avoids blurry scaling).
      if (canvas.width !== W || canvas.height !== H) {
        canvas.width = W;
        canvas.height = H;
      }

      if (!ctxRef.current) {
        ctxRef.current = canvas.getContext('2d');
      }
      const ctx = ctxRef.current;
      ctx.clearRect(0, 0, W, H);

      const midY = H / 2;
      const currentData = rxWaveDataRef.current;
      const currentPos = rxWaveWritePosRef.current;

      // Draw amplitude bars from center line.
      ctx.beginPath();
      ctx.strokeStyle = '#00e676';
      ctx.lineWidth = 1.5;

      for (let i = 0; i < RX_WAVE_HISTORY; i++) {
        const idx = (currentPos - RX_WAVE_HISTORY + i + RX_WAVE_HISTORY * 2) % RX_WAVE_HISTORY;
        const amp = currentData[idx];
        const x = (i / RX_WAVE_HISTORY) * W;
        const y1 = midY - amp * midY;
        const y2 = midY + amp * midY;
        ctx.moveTo(x, y1);
        ctx.lineTo(x, y2);
      }
      ctx.stroke();

      // Subtle center line.
      ctx.beginPath();
      ctx.strokeStyle = 'rgba(255,255,255,0.08)';
      ctx.lineWidth = 1;
      ctx.moveTo(0, midY);
      ctx.lineTo(W, midY);
      ctx.stroke();

      animRef.current = requestAnimationFrame(draw);
    }

    animRef.current = requestAnimationFrame(draw);
    return () => {
      if (animRef.current) cancelAnimationFrame(animRef.current);
    };
    // The rAF loop reads .current on every frame, so depending on the refs
    // themselves (which are stable) would just re-trigger the effect for no
    // reason.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (lattice) {
    return (
      <div className="rx-wave-lattice">
        <div className="rx-wave-label">
          <span>RX WAVEFORM</span>
          {sourceTag && <span className="rx-wave-src">· {sourceTag}</span>}
        </div>
        <div className="rx-wave-canvas" ref={wrapRef}>
          <canvas ref={canvasRef} />
        </div>
      </div>
    );
  }

  const content = (
    <>
      {!inline && <div className="card-title">Audio</div>}
      <div className="viz-row">
        <span className="viz-label">RX{sourceTag ? ` · ${sourceTag}` : ''}</span>
        <div className="viz-canvas-wrap" ref={wrapRef}>
          <canvas ref={canvasRef} />
        </div>
      </div>
    </>
  );

  if (inline) return content;
  return <div className="card">{content}</div>;
}
