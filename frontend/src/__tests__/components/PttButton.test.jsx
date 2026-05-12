// =============================================================================
// PttButton.test.jsx — Tests for push-to-talk button
// =============================================================================

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import PttButton from '../../components/PttButton.jsx';

function renderPtt(overrides = {}) {
  const defaultProps = {
    active: false,
    onDown: vi.fn(),
    onUp: vi.fn(),
    txChannels: [],
    channelAliases: {},
    voxEnabled: false,
    onVoxToggle: vi.fn(),
    ...overrides,
  };
  return { ...render(<PttButton {...defaultProps} />), props: defaultProps };
}

describe('TestPttButtonRender', () => {
  it('renders PTT button', () => {
    renderPtt();
    expect(screen.getByText('PTT')).toBeTruthy();
    expect(screen.getByText('Hold to talk')).toBeTruthy();
  });
});

describe('TestPttButtonActive', () => {
  it('shows active class when active prop is true', () => {
    const { container } = renderPtt({ active: true, txChannels: [1] });
    const btn = container.querySelector('.ptt-btn');
    expect(btn.classList.contains('active')).toBe(true);
  });

  it('does not show active class when active is false', () => {
    const { container } = renderPtt({ active: false, txChannels: [1] });
    const btn = container.querySelector('.ptt-btn');
    expect(btn.classList.contains('active')).toBe(false);
  });
});

describe('TestPttButtonTxInfo', () => {
  it('displays TX: none when no channels', () => {
    renderPtt({ txChannels: [] });
    expect(screen.getByText('none')).toBeTruthy();
  });

  it('displays specific channel numbers', () => {
    renderPtt({ txChannels: [1, 3] });
    expect(screen.getByText('Ch 1, Ch 3')).toBeTruthy();
  });

  it('displays All channels when all 5 are active', () => {
    renderPtt({ txChannels: [1, 2, 3, 4, 5] });
    expect(screen.getByText('All channels')).toBeTruthy();
  });

  it('displays channel aliases in TX info', () => {
    renderPtt({ txChannels: [1, 3], channelAliases: { 1: 'Command', 3: 'Fire' } });
    expect(screen.getByText('Command, Fire')).toBeTruthy();
  });
});

describe('TestPttButtonEvents', () => {
  it('calls onDown on mousedown', () => {
    const { container, props } = renderPtt({ txChannels: [1] });
    const btn = container.querySelector('.ptt-btn');
    fireEvent.mouseDown(btn);
    expect(props.onDown).toHaveBeenCalledTimes(1);
  });

  it('calls onUp on mouseup', () => {
    const { container, props } = renderPtt({ txChannels: [1] });
    const btn = container.querySelector('.ptt-btn');
    fireEvent.mouseUp(btn);
    expect(props.onUp).toHaveBeenCalledTimes(1);
  });

  it('calls onUp on mouseleave', () => {
    const { container, props } = renderPtt({ txChannels: [1] });
    const btn = container.querySelector('.ptt-btn');
    fireEvent.mouseLeave(btn);
    expect(props.onUp).toHaveBeenCalledTimes(1);
  });
});

describe('TestPttButtonVoxToggle', () => {
  it('renders VOX checkbox', () => {
    renderPtt();
    expect(screen.getByLabelText('VOX')).toBeTruthy();
  });

  it('VOX checkbox reflects voxEnabled prop', () => {
    renderPtt({ voxEnabled: true });
    const checkboxes = screen.getAllByLabelText('VOX');
    expect(checkboxes[0].checked).toBe(true);
  });

  it('calls onVoxToggle when VOX is toggled', () => {
    const { props } = renderPtt();
    const checkbox = screen.getByLabelText('VOX');
    fireEvent.click(checkbox);
    expect(props.onVoxToggle).toHaveBeenCalled();
  });
});
