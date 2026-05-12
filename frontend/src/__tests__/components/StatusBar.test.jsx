// =============================================================================
// StatusBar.test.jsx — Tests for WebSocket/Audio status indicators
// =============================================================================

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import StatusBar from '../../components/StatusBar.jsx';

describe('TestStatusBarConnected', () => {
  it('renders connected state with green dot', () => {
    const { container } = render(
      <StatusBar wsStatus="connected" audioStatus="Audio ready" />
    );
    expect(screen.getByText('Connected')).toBeTruthy();
    const dots = container.querySelectorAll('.dot-green');
    // Both WS and audio should show green
    expect(dots.length).toBe(2);
  });
});

describe('TestStatusBarDisconnected', () => {
  it('renders disconnected state with red dot', () => {
    const { container } = render(
      <StatusBar wsStatus="disconnected" audioStatus="Audio off" />
    );
    expect(screen.getByText('Disconnected')).toBeTruthy();
    const dots = container.querySelectorAll('.dot-red');
    // Both WS and audio should show red
    expect(dots.length).toBe(2);
  });
});

describe('TestStatusBarConnecting', () => {
  it('renders connecting state with yellow dot', () => {
    const { container } = render(
      <StatusBar wsStatus="connecting" audioStatus="Audio off" />
    );
    expect(screen.getByText('Connecting...')).toBeTruthy();
    const yellowDot = container.querySelector('.dot-yellow');
    expect(yellowDot).toBeTruthy();
  });
});

describe('TestStatusBarAudioText', () => {
  it('displays audio status text', () => {
    render(<StatusBar wsStatus="connected" audioStatus="Audio ready" />);
    expect(screen.getByText('Audio ready')).toBeTruthy();
  });

  it('displays custom audio status', () => {
    render(<StatusBar wsStatus="connected" audioStatus="Mic active" />);
    expect(screen.getByText('Mic active')).toBeTruthy();
  });
});
