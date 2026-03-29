// =============================================================================
// SettingsMenu.test.jsx — Tests for hamburger settings menu
// =============================================================================

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import SettingsMenu, { ALL_PANELS } from '../../components/SettingsMenu.jsx';

function renderMenu(overrides = {}) {
  const visibility = {};
  ALL_PANELS.forEach((k) => { visibility[k] = true; });
  const defaultProps = {
    panelVisibility: visibility,
    onTogglePanel: vi.fn(),
    ...overrides,
  };
  return { ...render(<SettingsMenu {...defaultProps} />), props: defaultProps };
}

describe('TestSettingsMenuRender', () => {
  it('renders hamburger button', () => {
    const { container } = renderMenu();
    expect(container.querySelector('.hamburger-btn')).toBeTruthy();
  });

  it('does not show dropdown initially', () => {
    renderMenu();
    expect(screen.queryByText('Panels')).toBeNull();
  });
});

describe('TestSettingsMenuOpen', () => {
  it('shows dropdown when hamburger is clicked', () => {
    const { container } = renderMenu();
    fireEvent.click(container.querySelector('.hamburger-btn'));
    expect(screen.getByText('Panels')).toBeTruthy();
  });

  it('shows all panel toggle options', () => {
    const { container } = renderMenu();
    fireEvent.click(container.querySelector('.hamburger-btn'));
    expect(screen.getByText('Channels')).toBeTruthy();
    expect(screen.getByText('PTT Button')).toBeTruthy();
    expect(screen.getByText('RX Waveform')).toBeTruthy();
    expect(screen.getByText('Mic Meter')).toBeTruthy();
    expect(screen.getByText('Audio Controls')).toBeTruthy();
    expect(screen.getByText('Audio File TX')).toBeTruthy();
    expect(screen.getByText('Mesh Status')).toBeTruthy();
    expect(screen.getByText('Network Topology')).toBeTruthy();
    expect(screen.getByText('Transcript')).toBeTruthy();
    expect(screen.getByText('System Log')).toBeTruthy();
  });
});

describe('TestSettingsMenuToggle', () => {
  it('calls onTogglePanel when a checkbox is clicked', () => {
    const { container, props } = renderMenu();
    fireEvent.click(container.querySelector('.hamburger-btn'));
    fireEvent.click(screen.getByText('System Log'));
    expect(props.onTogglePanel).toHaveBeenCalledWith('log');
  });

  it('checkbox reflects visibility state', () => {
    const visibility = {};
    ALL_PANELS.forEach((k) => { visibility[k] = true; });
    visibility.topology = false;
    const { container } = renderMenu({ panelVisibility: visibility });
    fireEvent.click(container.querySelector('.hamburger-btn'));

    const checkboxes = container.querySelectorAll('.settings-item input');
    const topoIdx = ALL_PANELS.indexOf('topology');
    expect(checkboxes[topoIdx].checked).toBe(false);
  });
});

describe('TestSettingsMenuAllPanels', () => {
  it('ALL_PANELS has 10 entries', () => {
    expect(ALL_PANELS.length).toBe(10);
  });
});
