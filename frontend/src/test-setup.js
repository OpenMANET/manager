import '@testing-library/jest-dom';

// Mock canvas getContext for jsdom (used by Sparkline, TopologyMap, etc.)
HTMLCanvasElement.prototype.getContext = function () {
  return {
    clearRect: () => {},
    beginPath: () => {},
    moveTo: () => {},
    lineTo: () => {},
    stroke: () => {},
    arc: () => {},
    fill: () => {},
    fillRect: () => {},
    fillText: () => {},
    measureText: () => ({ width: 0 }),
    setLineDash: () => {},
    scale: () => {},
    save: () => {},
    restore: () => {},
    translate: () => {},
    rotate: () => {},
    canvas: this,
    lineWidth: 1,
    strokeStyle: '',
    fillStyle: '',
    font: '',
    textAlign: '',
    textBaseline: '',
    globalAlpha: 1,
  };
};
