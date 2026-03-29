// =============================================================================
// LogBox.test.jsx — Tests for system log display
// =============================================================================

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import LogBox from '../../components/LogBox.jsx';

describe('TestLogBoxRender', () => {
  it('renders log entries', () => {
    const logs = [
      { msg: '[12:00:01] Connected', cls: 'info' },
      { msg: '[12:00:02] RX Ch1 from 10.0.0.1', cls: 'rx' },
    ];
    render(<LogBox logs={logs} />);
    expect(screen.getByText('[12:00:01] Connected')).toBeTruthy();
    expect(screen.getByText('[12:00:02] RX Ch1 from 10.0.0.1')).toBeTruthy();
  });

  it('renders empty log box without errors', () => {
    const { container } = render(<LogBox logs={[]} />);
    const logBox = container.querySelector('.log-box');
    expect(logBox).toBeTruthy();
    expect(logBox.children.length).toBe(0);
  });
});

describe('TestLogBoxCssClasses', () => {
  it('applies correct CSS classes for rx entries', () => {
    const logs = [{ msg: 'RX data', cls: 'rx' }];
    const { container } = render(<LogBox logs={logs} />);
    const entry = container.querySelector('.log-box > div');
    expect(entry.classList.contains('rx')).toBe(true);
  });

  it('applies correct CSS classes for tx entries', () => {
    const logs = [{ msg: 'TX data', cls: 'tx' }];
    const { container } = render(<LogBox logs={logs} />);
    const entry = container.querySelector('.log-box > div');
    expect(entry.classList.contains('tx')).toBe(true);
  });

  it('applies correct CSS classes for err entries', () => {
    const logs = [{ msg: 'Error occurred', cls: 'err' }];
    const { container } = render(<LogBox logs={logs} />);
    const entry = container.querySelector('.log-box > div');
    expect(entry.classList.contains('err')).toBe(true);
  });

  it('applies correct CSS classes for info entries', () => {
    const logs = [{ msg: 'Info message', cls: 'info' }];
    const { container } = render(<LogBox logs={logs} />);
    const entry = container.querySelector('.log-box > div');
    expect(entry.classList.contains('info')).toBe(true);
  });

  it('handles entries with no cls (empty class)', () => {
    const logs = [{ msg: 'Plain message' }];
    const { container } = render(<LogBox logs={logs} />);
    const entry = container.querySelector('.log-box > div');
    expect(entry.className).toBe('');
  });
});
