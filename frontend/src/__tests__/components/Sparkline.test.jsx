// =============================================================================
// Sparkline.test.jsx — Tests for sparkline canvas component
// =============================================================================

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import Sparkline from '../../components/Sparkline.jsx';

describe('TestSparklineRender', () => {
  it('renders a canvas element', () => {
    const { container } = render(
      <Sparkline data={[-60, -55, -50, -48]} width={60} height={16} color="#6B8E23" />
    );
    const canvas = container.querySelector('canvas');
    expect(canvas).toBeTruthy();
  });

  it('sets canvas dimensions from props', () => {
    const { container } = render(
      <Sparkline data={[-60, -55]} width={80} height={20} color="#6B8E23" />
    );
    const canvas = container.querySelector('canvas');
    expect(canvas.style.width).toBe('80px');
    expect(canvas.style.height).toBe('20px');
  });

  it('renders without crashing with minimal data', () => {
    const { container } = render(
      <Sparkline data={[1]} width={60} height={16} color="#fff" />
    );
    expect(container.querySelector('canvas')).toBeTruthy();
  });

  it('renders without crashing with empty data', () => {
    const { container } = render(
      <Sparkline data={[]} width={60} height={16} color="#fff" />
    );
    expect(container.querySelector('canvas')).toBeTruthy();
  });

  it('renders without crashing with null data', () => {
    const { container } = render(
      <Sparkline data={null} width={60} height={16} color="#fff" />
    );
    expect(container.querySelector('canvas')).toBeTruthy();
  });
});

describe('TestSparklineMinMax', () => {
  it('accepts min and max bounds', () => {
    // Should not throw with explicit bounds.
    const { container } = render(
      <Sparkline data={[-70, -60, -50]} min={-100} max={-30} width={60} height={16} color="#6B8E23" />
    );
    expect(container.querySelector('canvas')).toBeTruthy();
  });
});
