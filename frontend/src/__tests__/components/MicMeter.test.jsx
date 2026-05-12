// =============================================================================
// MicMeter.test.jsx — Tests for microphone level meter
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, cleanup, act } from '@testing-library/react';
import MicMeter from '../../components/MicMeter.jsx';

let rafCallbacks = [];
let rafId = 0;

beforeEach(() => {
  rafCallbacks = [];
  rafId = 0;
  vi.stubGlobal('requestAnimationFrame', (cb) => {
    rafCallbacks.push(cb);
    return ++rafId;
  });
  vi.stubGlobal('cancelAnimationFrame', vi.fn());
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function flushRaf(count = 1) {
  for (let i = 0; i < count; i++) {
    const cbs = [...rafCallbacks];
    rafCallbacks = [];
    cbs.forEach(cb => cb());
  }
}

function renderMeter(props = {}) {
  const defaults = { level: 0, active: false, voxEnabled: false };
  return render(<MicMeter {...defaults} {...props} />);
}

describe('TestMicMeterLabel', () => {
  it('always renders MIC label', () => {
    const { container } = renderMeter();
    expect(container.querySelector('.viz-label').textContent).toBe('MIC');
  });
});

describe('TestMicMeterVisibility', () => {
  it('meter bar at 0% when inactive and no vox', () => {
    const { container } = renderMeter({ level: 0.5, active: false, voxEnabled: false });
    const bar = container.querySelector('.meter-bar');
    expect(bar.style.width).toBe('0%');
  });

  it('meter shows non-zero width when active', () => {
    const { container } = renderMeter({ level: 0.5, active: true });
    act(() => flushRaf(2));
    const bar = container.querySelector('.meter-bar');
    const width = parseFloat(bar.style.width);
    expect(width).toBeGreaterThan(0);
  });

  it('meter shows non-zero width when voxEnabled', () => {
    const { container } = renderMeter({ level: 0.3, active: false, voxEnabled: true });
    act(() => flushRaf(2));
    const bar = container.querySelector('.meter-bar');
    const width = parseFloat(bar.style.width);
    expect(width).toBeGreaterThan(0);
  });
});

describe('TestMicMeterDb', () => {
  it('shows dB for significant level', () => {
    const { container } = renderMeter({ level: 0.1, active: true });
    act(() => flushRaf(2));
    const dbEl = container.querySelector('.meter-db');
    expect(dbEl.textContent).toContain('dB');
  });

  it('shows empty dB for near-zero level', () => {
    const { container } = renderMeter({ level: 0.00001, active: true });
    act(() => flushRaf(2));
    const dbEl = container.querySelector('.meter-db');
    expect(dbEl.textContent).toBe('');
  });
});

describe('TestMicMeterCleanup', () => {
  it('cancels animation frame on unmount', () => {
    const { unmount } = renderMeter({ level: 0.5, active: true });
    unmount();
    expect(cancelAnimationFrame).toHaveBeenCalled();
  });
});
