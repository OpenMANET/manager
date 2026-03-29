// =============================================================================
// Transcript.test.jsx — Tests for chat transcript component
// =============================================================================

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import Transcript from '../../components/Transcript.jsx';

function renderTranscript(overrides = {}) {
  const defaultProps = {
    messages: [],
    whisperEnabled: false,
    onWhisperToggle: vi.fn(),
    debugMode: false,
    onDebugToggle: vi.fn(),
    whisperStatus: '',
    activeFilter: 0,
    onFilterChange: vi.fn(),
    channelAliases: {},
    ...overrides,
  };
  return { ...render(<Transcript {...defaultProps} />), props: defaultProps };
}

describe('TestTranscriptRenderMessages', () => {
  it('renders messages with correct channel badges', () => {
    const messages = [
      { ch: 1, ip: '10.0.0.1', text: 'Hello from ch1', ts: '12:00:01' },
      { ch: 3, ip: '10.0.0.3', text: 'Hello from ch3', ts: '12:00:02' },
    ];
    const { container } = renderTranscript({ messages });

    expect(screen.getByText('Hello from ch1')).toBeTruthy();
    expect(screen.getByText('Hello from ch3')).toBeTruthy();
    expect(screen.getByText('Ch1')).toBeTruthy();
    expect(screen.getByText('Ch3')).toBeTruthy();

    // Check channel badge CSS classes
    const ch1Badge = container.querySelector('.chat-ch-1');
    expect(ch1Badge).toBeTruthy();
    const ch3Badge = container.querySelector('.chat-ch-3');
    expect(ch3Badge).toBeTruthy();
  });

  it('renders timestamps and IPs', () => {
    const messages = [
      { ch: 2, ip: '192.168.1.5', text: 'Test', ts: '14:30:00' },
    ];
    renderTranscript({ messages });
    expect(screen.getByText('14:30:00')).toBeTruthy();
    expect(screen.getByText('192.168.1.5')).toBeTruthy();
  });
});

describe('TestTranscriptFilter', () => {
  it('hides messages not matching filter', () => {
    const messages = [
      { ch: 1, ip: '10.0.0.1', text: 'Visible', ts: '12:00:01' },
      { ch: 2, ip: '10.0.0.2', text: 'Hidden', ts: '12:00:02' },
    ];
    const { container } = renderTranscript({ messages, activeFilter: 1 });

    const msgDivs = container.querySelectorAll('.chat-msg');
    expect(msgDivs[0].classList.contains('hidden')).toBe(false);
    expect(msgDivs[1].classList.contains('hidden')).toBe(true);
  });

  it('shows all messages when filter is 0', () => {
    const messages = [
      { ch: 1, ip: '10.0.0.1', text: 'Msg1', ts: '12:00:01' },
      { ch: 2, ip: '10.0.0.2', text: 'Msg2', ts: '12:00:02' },
    ];
    const { container } = renderTranscript({ messages, activeFilter: 0 });

    const hidden = container.querySelectorAll('.chat-msg.hidden');
    expect(hidden.length).toBe(0);
  });

  it('calls onFilterChange when select changes', () => {
    const { props } = renderTranscript();
    const select = screen.getByRole('combobox');
    fireEvent.change(select, { target: { value: '3' } });
    expect(props.onFilterChange).toHaveBeenCalledWith(3);
  });
});

describe('TestTranscriptFilterAliases', () => {
  it('shows channel aliases in filter dropdown', () => {
    renderTranscript({ channelAliases: { 1: 'Command', 4: 'Logistics' } });
    const select = screen.getByRole('combobox');
    const options = select.querySelectorAll('option');
    // "All Channels" + 5 channels = 6 options
    expect(options.length).toBe(6);
    expect(options[1].textContent).toBe('Command');
    expect(options[2].textContent).toBe('Ch 2');
    expect(options[4].textContent).toBe('Logistics');
  });
});

describe('TestTranscriptWhisperToggle', () => {
  it('shows whisper toggle checkbox', () => {
    renderTranscript();
    const checkbox = screen.getByLabelText(/closed captions/i);
    expect(checkbox).toBeTruthy();
    expect(checkbox.type).toBe('checkbox');
  });

  it('calls onWhisperToggle when checkbox changes', () => {
    const { props } = renderTranscript();
    const checkbox = screen.getByLabelText(/closed captions/i);
    fireEvent.click(checkbox);
    expect(props.onWhisperToggle).toHaveBeenCalled();
  });

  it('reflects whisperEnabled prop', () => {
    renderTranscript({ whisperEnabled: true });
    const checkbox = screen.getByLabelText(/closed captions/i);
    expect(checkbox.checked).toBe(true);
  });
});

describe('TestTranscriptTextEscaping', () => {
  it('renders text content safely via React (no raw HTML injection)', () => {
    const messages = [
      { ch: 1, ip: '10.0.0.1', text: '<script>alert("xss")</script>', ts: '12:00:00' },
    ];
    const { container } = renderTranscript({ messages });

    expect(container.querySelector('script')).toBeNull();
    expect(screen.getByText('<script>alert("xss")</script>')).toBeTruthy();
  });
});
