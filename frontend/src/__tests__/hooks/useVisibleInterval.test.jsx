// =============================================================================
// useVisibleInterval.test.jsx — tests for visibility-gated polling hook
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/react';
import { useVisibleInterval } from '../../hooks/useVisibleInterval.js';

function Harness({ cb, intervalMs }) {
  useVisibleInterval(cb, intervalMs);
  return null;
}

function setVisibility(state) {
  Object.defineProperty(document, 'visibilityState', {
    value: state,
    configurable: true,
    writable: true,
  });
  Object.defineProperty(document, 'hidden', {
    value: state === 'hidden',
    configurable: true,
    writable: true,
  });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('TestUseVisibleInterval', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setVisibility('visible');
  });

  afterEach(() => {
    vi.useRealTimers();
    setVisibility('visible');
  });

  it('fires the callback once on mount', () => {
    const cb = vi.fn();
    render(<Harness cb={cb} intervalMs={1000} />);
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it('fires the callback on every interval while visible', () => {
    const cb = vi.fn();
    render(<Harness cb={cb} intervalMs={1000} />);
    expect(cb).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(3000);
    expect(cb).toHaveBeenCalledTimes(4);
  });

  it('stops firing when the tab becomes hidden', () => {
    const cb = vi.fn();
    render(<Harness cb={cb} intervalMs={1000} />);
    cb.mockClear();

    setVisibility('hidden');
    vi.advanceTimersByTime(5000);
    expect(cb).toHaveBeenCalledTimes(0);
  });

  it('resumes with an immediate fire when the tab becomes visible', () => {
    const cb = vi.fn();
    render(<Harness cb={cb} intervalMs={1000} />);
    cb.mockClear();

    setVisibility('hidden');
    vi.advanceTimersByTime(5000);
    expect(cb).toHaveBeenCalledTimes(0);

    setVisibility('visible');
    expect(cb).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(2000);
    expect(cb).toHaveBeenCalledTimes(3);
  });

  it('clears the interval on unmount', () => {
    const cb = vi.fn();
    const { unmount } = render(<Harness cb={cb} intervalMs={1000} />);
    cb.mockClear();

    unmount();
    vi.advanceTimersByTime(5000);
    expect(cb).toHaveBeenCalledTimes(0);
  });

  it('does nothing when intervalMs is 0 or null', () => {
    const cb = vi.fn();
    render(<Harness cb={cb} intervalMs={0} />);
    vi.advanceTimersByTime(5000);
    expect(cb).toHaveBeenCalledTimes(0);
  });
});
