import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';

vi.mock('../../components/Terminal.jsx', () => ({
  default: vi.fn(({ onClose }) => {
    // Expose a handler the test can trigger.
    window.__triggerTerminalClose = onClose;
    return <div data-testid="mock-terminal" />;
  }),
}));

import SettingsTerminal from '../../pages/SettingsTerminal.jsx';

describe('SettingsTerminal', () => {
  beforeEach(() => {
    delete window.__triggerTerminalClose;
  });

  it('renders the Terminal by default', () => {
    render(<SettingsTerminal />);
    expect(screen.getByTestId('mock-terminal')).toBeInTheDocument();
  });

  it('shows the "in use" banner when close code is 1008', () => {
    render(<SettingsTerminal />);
    act(() => { window.__triggerTerminalClose?.({ code: 1008, reason: 'terminal already in use' }); });
    expect(screen.getByText(/already in use/i)).toBeInTheDocument();
    expect(screen.queryByTestId('mock-terminal')).not.toBeInTheDocument();
  });

  it('Reconnect remounts the terminal after a close', () => {
    render(<SettingsTerminal />);
    act(() => { window.__triggerTerminalClose?.({ code: 1008, reason: 'in use' }); });
    expect(screen.queryByTestId('mock-terminal')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /reconnect/i }));
    expect(screen.getByTestId('mock-terminal')).toBeInTheDocument();
  });
});
