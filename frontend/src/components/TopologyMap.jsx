// =============================================================================
// TopologyMap.jsx — Canvas-based mesh network topology visualization
// =============================================================================
// Shows the local node at center with neighbors arranged radially. Lines
// colored by signal strength, sized by throughput.

import React, { useRef, useEffect } from 'react';

function signalColor(dBm) {
  if (dBm > -60) return '#6B8E23';     // green — strong
  if (dBm > -75) return '#b8a000';     // yellow — moderate
  return '#cc3333';                     // red — weak
}

export default React.memo(function TopologyMap({ data }) {
  const canvasRef = useRef(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.parentElement.getBoundingClientRect();
    const w = rect.width;
    const h = 260;
    canvas.width = w * dpr;
    canvas.height = h * dpr;
    canvas.style.width = w + 'px';
    canvas.style.height = h + 'px';
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, w, h);

    const cx = w / 2;
    const cy = h / 2;

    const neighbors = (data && data.neighbors && Array.isArray(data.neighbors)) ? data.neighbors : [];
    const nodes = (data && data.nodes && Array.isArray(data.nodes)) ? data.nodes : [];

    // Draw self node
    ctx.beginPath();
    ctx.arc(cx, cy, 16, 0, Math.PI * 2);
    ctx.fillStyle = '#6B8E23';
    ctx.fill();
    ctx.fillStyle = '#e7ebf2';
    ctx.font = 'bold 9px -apple-system, sans-serif';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('SELF', cx, cy);

    if (neighbors.length === 0 && nodes.length === 0) {
      ctx.fillStyle = '#9aa7b3';
      ctx.font = '11px -apple-system, sans-serif';
      ctx.fillText('No nodes discovered', cx, cy + 36);
      return;
    }

    // Place neighbors in inner ring
    const innerR = Math.min(w, h) * 0.32;
    const neighborPositions = [];
    neighbors.forEach((n, i) => {
      const angle = (i / Math.max(neighbors.length, 1)) * Math.PI * 2 - Math.PI / 2;
      const nx = cx + Math.cos(angle) * innerR;
      const ny = cy + Math.sin(angle) * innerR;
      neighborPositions.push({ x: nx, y: ny, neighbor: n });

      // Draw link line
      const col = signalColor(n.signal || -100);
      const throughput = n.throughput || 0;
      const lineWidth = Math.max(1, Math.min(4, throughput / 500000));
      ctx.beginPath();
      ctx.moveTo(cx, cy);
      ctx.lineTo(nx, ny);
      ctx.strokeStyle = col;
      ctx.lineWidth = lineWidth;
      ctx.globalAlpha = 0.6;
      ctx.stroke();
      ctx.globalAlpha = 1;

      // Draw neighbor node
      ctx.beginPath();
      ctx.arc(nx, ny, 10, 0, Math.PI * 2);
      ctx.fillStyle = col;
      ctx.fill();

      // Label
      const label = n.name || n.mac || '?';
      ctx.fillStyle = '#e7ebf2';
      ctx.font = '9px -apple-system, sans-serif';
      ctx.textAlign = 'center';
      ctx.fillText(label, nx, ny + 18);

      // Signal label
      ctx.fillStyle = '#9aa7b3';
      ctx.font = '8px -apple-system, sans-serif';
      ctx.fillText((n.signal || '?') + ' dBm', nx, ny + 28);
    });

    // Place non-neighbor nodes in outer ring
    const neighborNames = new Set(neighbors.map((n) => n.name || ''));
    const outerNodes = nodes.filter((n) => !neighborNames.has(n.hostname));
    const outerR = Math.min(w, h) * 0.44;
    outerNodes.forEach((n, i) => {
      const angle = (i / Math.max(outerNodes.length, 1)) * Math.PI * 2 - Math.PI / 2;
      const nx = cx + Math.cos(angle) * outerR;
      const ny = cy + Math.sin(angle) * outerR;

      ctx.beginPath();
      ctx.arc(nx, ny, 7, 0, Math.PI * 2);
      ctx.fillStyle = '#555';
      ctx.fill();

      ctx.fillStyle = '#9aa7b3';
      ctx.font = '8px -apple-system, sans-serif';
      ctx.textAlign = 'center';
      ctx.fillText(n.hostname || n.ip || '?', nx, ny + 15);
    });
  }, [data]);

  return (
    <div className="card span-2">
      <div className="card-title">Network Topology</div>
      <div style={{ width: '100%', height: '220px', position: 'relative' }}>
        <canvas ref={canvasRef} style={{ display: 'block' }} />
      </div>
    </div>
  );
});
