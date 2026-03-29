// =============================================================================
// AudioControls.test.jsx — Tests for speaker/mic volume sliders + VOX
// =============================================================================

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import AudioControls from '../../components/AudioControls.jsx';

function renderControls(overrides = {}) {
  const defaultProps = {
    speakerVol: 75,
    micVol: 50,
    onSpeakerChange: vi.fn(),
    onMicChange: vi.fn(),
    voxEnabled: false,
    voxThreshold: 0.15,
    onVoxToggle: vi.fn(),
    onVoxThresholdChange: vi.fn(),
    ...overrides,
  };
  return { ...render(<AudioControls {...defaultProps} />), props: defaultProps };
}

describe('TestAudioControlsRender', () => {
  it('renders speaker and mic sliders', () => {
    renderControls();
    expect(screen.getByText('Speaker:')).toBeTruthy();
    expect(screen.getByText('Mic:')).toBeTruthy();

    const sliders = screen.getAllByRole('slider');
    expect(sliders.length).toBeGreaterThanOrEqual(2);
  });

  it('sets correct initial values', () => {
    renderControls({ speakerVol: 80, micVol: 40 });
    const sliders = screen.getAllByRole('slider');
    expect(sliders[0].value).toBe('80');
    expect(sliders[1].value).toBe('40');
  });
});

describe('TestAudioControlsOnChange', () => {
  it('calls onSpeakerChange with correct value', () => {
    const { props } = renderControls();
    const sliders = screen.getAllByRole('slider');
    fireEvent.change(sliders[0], { target: { value: '90' } });
    expect(props.onSpeakerChange).toHaveBeenCalledWith(90);
  });

  it('calls onMicChange with correct value', () => {
    const { props } = renderControls();
    const sliders = screen.getAllByRole('slider');
    fireEvent.change(sliders[1], { target: { value: '25' } });
    expect(props.onMicChange).toHaveBeenCalledWith(25);
  });

  it('passes numeric values not strings', () => {
    const { props } = renderControls();
    const sliders = screen.getAllByRole('slider');
    fireEvent.change(sliders[0], { target: { value: '0' } });
    expect(props.onSpeakerChange).toHaveBeenCalledWith(0);
    expect(typeof props.onSpeakerChange.mock.calls[0][0]).toBe('number');
  });
});

describe('TestAudioControlsVox', () => {
  it('renders VOX checkbox', () => {
    renderControls();
    expect(screen.getByLabelText('VOX')).toBeTruthy();
  });

  it('VOX checkbox reflects voxEnabled prop', () => {
    renderControls({ voxEnabled: true });
    expect(screen.getByLabelText('VOX').checked).toBe(true);
  });

  it('calls onVoxToggle when VOX checkbox changes', () => {
    const { props } = renderControls();
    fireEvent.click(screen.getByLabelText('VOX'));
    expect(props.onVoxToggle).toHaveBeenCalled();
  });

  it('shows threshold slider when VOX is enabled', () => {
    renderControls({ voxEnabled: true });
    expect(screen.getByText('Threshold:')).toBeTruthy();
    const sliders = screen.getAllByRole('slider');
    // Speaker + Mic + VOX threshold = 3 sliders
    expect(sliders.length).toBe(3);
  });

  it('hides threshold slider when VOX is disabled', () => {
    renderControls({ voxEnabled: false });
    expect(screen.queryByText('Threshold:')).toBeNull();
    const sliders = screen.getAllByRole('slider');
    expect(sliders.length).toBe(2);
  });
});
