// =============================================================================
// RxWaveform.test.jsx — Tests for RX audio waveform visualizer
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import RxWaveform from '../../components/RxWaveform.jsx';

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (cb) => 1);
  vi.stubGlobal('cancelAnimationFrame', vi.fn());
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const waveData = new Float32Array(200);
const defaultProps = { rxWaveData: waveData, writePos: 0 };

describe('TestRxWaveformRender', () => {
  it('renders canvas element', () => {
    const { container } = render(<RxWaveform {...defaultProps} />);
    expect(container.querySelector('canvas')).toBeTruthy();
  });

  it('renders RX label', () => {
    const { container } = render(<RxWaveform {...defaultProps} />);
    expect(container.querySelector('.viz-label').textContent).toBe('RX');
  });
});

describe('TestRxWaveformInline', () => {
  it('renders card wrapper when not inline', () => {
    const { container } = render(<RxWaveform {...defaultProps} inline={false} />);
    expect(container.querySelector('.card')).toBeTruthy();
    expect(container.querySelector('.card-title').textContent).toBe('Audio');
  });

  it('omits card wrapper when inline', () => {
    const { container } = render(<RxWaveform {...defaultProps} inline={true} />);
    expect(container.querySelector('.card')).toBeNull();
    expect(container.querySelector('.card-title')).toBeNull();
  });
});

describe('TestRxWaveformCleanup', () => {
  it('cancels animation frame on unmount', () => {
    const { unmount } = render(<RxWaveform {...defaultProps} />);
    unmount();
    expect(cancelAnimationFrame).toHaveBeenCalled();
  });
});
