// =============================================================================
// Sparkline.jsx — Tiny canvas sparkline for signal/throughput history
// =============================================================================

import React, { useRef, useEffect } from 'react';

export default function Sparkline({ data, width = 60, height = 16, color = '#00e676', min, max }) {
  const canvasRef = useRef(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !data || data.length < 2) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, width, height);

    const lo = min !== undefined ? min : Math.min(...data);
    const hi = max !== undefined ? max : Math.max(...data);
    const range = hi - lo || 1;

    ctx.beginPath();
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.2;
    for (let i = 0; i < data.length; i++) {
      const x = (i / (data.length - 1)) * width;
      const y = height - ((data[i] - lo) / range) * (height - 2) - 1;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.stroke();
  }, [data, width, height, color, min, max]);

  return (
    <canvas
      ref={canvasRef}
      style={{ width: `${width}px`, height: `${height}px`, display: 'inline-block', verticalAlign: 'middle' }}
    />
  );
}
