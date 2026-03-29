// =============================================================================
// ChannelGrid.test.jsx — Tests for channel RX/TX toggle grid
// =============================================================================

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ChannelGrid from '../../components/ChannelGrid.jsx';
import { CHANNELS_DEF } from '../../constants.js';

function renderGrid(overrides = {}) {
  const defaultProps = {
    channels: CHANNELS_DEF,
    rxEnabled: { 1: false, 2: false, 3: false, 4: false, 5: false },
    txEnabled: { 1: false, 2: false, 3: false, 4: false, 5: false },
    rxLastTimeRef: { current: {} },
    onToggleRx: vi.fn(),
    onToggleTx: vi.fn(),
    onRxAll: vi.fn(),
    onTxAll: vi.fn(),
    channelAliases: {},
    onAliasChange: vi.fn(),
    onReplay: vi.fn(),
    replayAvailable: {},
    ...overrides,
  };
  return { ...render(<ChannelGrid {...defaultProps} />), props: defaultProps };
}

describe('TestChannelGridRender', () => {
  it('renders all 5 channels', () => {
    renderGrid();
    for (let i = 1; i <= 5; i++) {
      expect(screen.getByText(`Ch ${i}`)).toBeTruthy();
    }
  });

  it('shows port numbers', () => {
    renderGrid();
    expect(screen.getByText('port 38801')).toBeTruthy();
    expect(screen.getByText('port 38803')).toBeTruthy();
    expect(screen.getByText('port 38805')).toBeTruthy();
    expect(screen.getByText('port 38807')).toBeTruthy();
    expect(screen.getByText('port 38809')).toBeTruthy();
  });
});

describe('TestChannelGridRxToggle', () => {
  it('RX button applies rx-on class when enabled', () => {
    renderGrid({
      rxEnabled: { 1: true, 2: false, 3: false, 4: false, 5: false },
    });

    const rxButtons = screen.getAllByText('RX');
    expect(rxButtons[0].classList.contains('rx-on')).toBe(true);
    expect(rxButtons[1].classList.contains('rx-on')).toBe(false);
  });

  it('calls onToggleRx when RX button is clicked', () => {
    const { props } = renderGrid();
    const rxButtons = screen.getAllByText('RX');
    fireEvent.click(rxButtons[2]); // Click Ch 3 RX
    expect(props.onToggleRx).toHaveBeenCalledWith(3);
  });
});

describe('TestChannelGridTxToggle', () => {
  it('TX button calls onToggleTx', () => {
    const { props } = renderGrid();
    const txButtons = screen.getAllByText('TX');
    fireEvent.click(txButtons[0]); // Click Ch 1 TX
    expect(props.onToggleTx).toHaveBeenCalledWith(1);
  });

  it('TX button applies tx-on class when enabled', () => {
    renderGrid({
      txEnabled: { 1: false, 2: true, 3: false, 4: false, 5: false },
    });
    const txButtons = screen.getAllByText('TX');
    expect(txButtons[1].classList.contains('tx-on')).toBe(true);
    expect(txButtons[0].classList.contains('tx-on')).toBe(false);
  });
});

describe('TestChannelGridBulkButtons', () => {
  it('RX All calls onRxAll(true)', () => {
    const { props } = renderGrid();
    fireEvent.click(screen.getByText('RX All'));
    expect(props.onRxAll).toHaveBeenCalledWith(true);
  });

  it('RX None calls onRxAll(false)', () => {
    const { props } = renderGrid();
    fireEvent.click(screen.getByText('RX None'));
    expect(props.onRxAll).toHaveBeenCalledWith(false);
  });

  it('TX All calls onTxAll(true)', () => {
    const { props } = renderGrid();
    fireEvent.click(screen.getByText('TX All'));
    expect(props.onTxAll).toHaveBeenCalledWith(true);
  });

  it('TX None calls onTxAll(false)', () => {
    const { props } = renderGrid();
    fireEvent.click(screen.getByText('TX None'));
    expect(props.onTxAll).toHaveBeenCalledWith(false);
  });
});

describe('TestChannelGridAliases', () => {
  it('shows alias instead of default label', () => {
    renderGrid({ channelAliases: { 1: 'Command', 3: 'Fire Net' } });
    expect(screen.getByText('Command')).toBeTruthy();
    expect(screen.getByText('Fire Net')).toBeTruthy();
    expect(screen.getByText('Ch 2')).toBeTruthy();
  });

  it('shows default label when alias is empty', () => {
    renderGrid({ channelAliases: {} });
    expect(screen.getByText('Ch 1')).toBeTruthy();
  });
});

describe('TestChannelGridReplay', () => {
  it('renders replay buttons for each channel', () => {
    const { container } = renderGrid();
    const replayBtns = container.querySelectorAll('.ch-replay-btn');
    expect(replayBtns.length).toBe(5);
  });

  it('replay button is disabled when no data available', () => {
    const { container } = renderGrid({ replayAvailable: {} });
    const replayBtns = container.querySelectorAll('.ch-replay-btn');
    replayBtns.forEach((btn) => {
      expect(btn.disabled).toBe(true);
    });
  });

  it('replay button is enabled when data available', () => {
    const { container } = renderGrid({ replayAvailable: { 1: true } });
    const replayBtns = container.querySelectorAll('.ch-replay-btn');
    expect(replayBtns[0].disabled).toBe(false);
    expect(replayBtns[1].disabled).toBe(true);
  });

  it('calls onReplay with channel number when clicked', () => {
    const { props, container } = renderGrid({ replayAvailable: { 2: true } });
    const replayBtns = container.querySelectorAll('.ch-replay-btn');
    fireEvent.click(replayBtns[1]); // Ch 2 replay
    expect(props.onReplay).toHaveBeenCalledWith(2);
  });
});
