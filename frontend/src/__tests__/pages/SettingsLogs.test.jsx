import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act, cleanup } from '@testing-library/react';
import { createRouterTransport, Code, ConnectError } from '@connectrpc/connect';
import { LogsService } from '../../gen/openmanet/logs/v1/logs_service_connect.js';

// Mock the connectClient module with a router transport that each test
// can override via a module-level `nextResponses` object.
const transportState = {
  logreadResponse: null,
  dmesgResponse: null,
  errorForSource: null, // { source: 'logread'|'dmesg', code, message }
  callCount: 0,
  lastRequest: null,
};

function buildTransport() {
  return createRouterTransport(({ service }) => {
    service(LogsService, {
      getLogs(req) {
        transportState.callCount += 1;
        transportState.lastRequest = req;
        const err = transportState.errorForSource;
        if (err) {
          throw new ConnectError(err.message, err.code);
        }
        // req.source is an enum number (1 = LOGREAD, 2 = DMESG)
        if (req.source === 1) {
          return transportState.logreadResponse || { lines: [], truncated: false };
        }
        if (req.source === 2) {
          return transportState.dmesgResponse || { lines: [], truncated: false };
        }
        return { lines: [], truncated: false };
      },
    });
  });
}

vi.mock('../../services/connectClient.js', () => ({ transport: buildTransport() }));

import SettingsLogs from '../../pages/SettingsLogs.jsx';

function resetTransportState() {
  transportState.logreadResponse = null;
  transportState.dmesgResponse = null;
  transportState.errorForSource = null;
  transportState.callCount = 0;
  transportState.lastRequest = null;
}

async function flushPromises() {
  // Exhaust microtasks from the pending fetchLogs promise.
  for (let i = 0; i < 5; i++) {
    await Promise.resolve();
  }
}

describe('SettingsLogs', () => {
  beforeEach(() => {
    resetTransportState();
    window.localStorage?.clear?.();
    // URL.createObjectURL isn't available in jsdom.
    globalThis.URL.createObjectURL = vi.fn(() => 'blob:mock');
    globalThis.URL.revokeObjectURL = vi.fn();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('renders both source pills and refresh controls', async () => {
    transportState.logreadResponse = { lines: [{ raw: 'line-1' }, { raw: 'line-2' }], truncated: false };
    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    expect(screen.getByRole('tab', { name: /logread/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /dmesg/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/auto-refresh interval/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /refresh now/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /download/i })).toBeInTheDocument();
  });

  it('fetches logread on mount and renders the lines', async () => {
    transportState.logreadResponse = {
      lines: [{ raw: 'syslog A' }, { raw: 'syslog B' }],
      truncated: false,
    };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    expect(transportState.callCount).toBeGreaterThanOrEqual(1);
    expect(transportState.lastRequest?.source).toBe(1); // LOGREAD
    expect(screen.getByText(/syslog A/)).toBeInTheDocument();
    expect(screen.getByText(/syslog B/)).toBeInTheDocument();
  });

  it('switches to dmesg when the dmesg pill is clicked', async () => {
    transportState.logreadResponse = { lines: [{ raw: 'ls' }], truncated: false };
    transportState.dmesgResponse = { lines: [{ raw: 'KERNEL-LINE' }], truncated: false };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('tab', { name: /dmesg/i }));
      await flushPromises();
    });

    expect(transportState.lastRequest?.source).toBe(2); // DMESG
    expect(screen.getByText(/KERNEL-LINE/)).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /dmesg/i })).toHaveAttribute('aria-selected', 'true');
  });

  it('strips ANSI escape codes from incoming log lines', async () => {
    const ESC = String.fromCharCode(27);
    transportState.logreadResponse = {
      lines: [
        { raw: '[32mINF[0m mgmt [1mAlfred Client Started[0m' },
        { raw: '[36maddr=[0m0.0.0.0:8080' },
      ],
      truncated: false,
    };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    expect(screen.getByText(/INF mgmt Alfred Client Started/)).toBeInTheDocument();
    expect(screen.getByText(/addr=0\.0\.0\.0:8080/)).toBeInTheDocument();
    // No raw escape sequences should remain in the rendered <pre>.
    const pre = document.querySelector(".settings-logs-pre");
    expect(pre.textContent).not.toContain(ESC);
    expect(pre.textContent).not.toMatch(/\[3[0-9]m/);
  });

  it('renders a "last N truncated" notice when the response is truncated', async () => {
    transportState.logreadResponse = {
      lines: [{ raw: 'x' }],
      truncated: true,
    };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    expect(screen.getByText(/older truncated/i)).toBeInTheDocument();
  });

  it('shows a friendly banner when the source is unavailable (FailedPrecondition)', async () => {
    transportState.errorForSource = {
      source: 'logread',
      code: Code.FailedPrecondition,
      message: 'logread binary unavailable on this host',
    };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    expect(screen.getByText(/not available on this host/i)).toBeInTheDocument();
  });

  it('manual refresh triggers an additional fetch', async () => {
    transportState.logreadResponse = { lines: [{ raw: 'a' }], truncated: false };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    const callsBefore = transportState.callCount;

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /refresh now/i }));
      await flushPromises();
    });

    expect(transportState.callCount).toBe(callsBefore + 1);
  });

  it('download button builds a Blob with the current content', async () => {
    transportState.logreadResponse = {
      lines: [{ raw: 'download-line-1' }, { raw: 'download-line-2' }],
      truncated: false,
    };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /download/i }));
      await flushPromises();
    });

    expect(globalThis.URL.createObjectURL).toHaveBeenCalledTimes(1);
    const blobArg = globalThis.URL.createObjectURL.mock.calls[0][0];
    expect(blobArg).toBeInstanceOf(Blob);
  });

  it('interval dropdown persists the selected value to localStorage', async () => {
    transportState.logreadResponse = { lines: [], truncated: false };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    const trigger = screen.getByRole('button', { name: /auto-refresh interval/i });

    await act(async () => {
      fireEvent.click(trigger);
      await flushPromises();
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('option', { name: '30s' }));
      await flushPromises();
    });

    expect(window.localStorage.getItem('openmanetd.logs.intervalMs')).toBe('30000');
  });

  it('disables the download button when no lines are loaded', async () => {
    transportState.logreadResponse = { lines: [], truncated: false };

    await act(async () => {
      render(<SettingsLogs />);
      await flushPromises();
    });

    expect(screen.getByRole('button', { name: /download/i })).toBeDisabled();
  });
});
