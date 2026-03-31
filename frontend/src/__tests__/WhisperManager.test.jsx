// =============================================================================
// WhisperManager.test.jsx — Tests for Whisper download/remove settings card
// =============================================================================

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import WhisperManager from '../components/WhisperManager.jsx';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function mockFetch(handler) {
  vi.stubGlobal('fetch', vi.fn(handler));
}

// Returns a fetch mock that responds based on URL.
function statusResponse(overrides = {}) {
  return {
    available: false, state: 'idle', progress: 0, error: '',
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Setup / Teardown
// ---------------------------------------------------------------------------

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('WhisperManager', () => {
  it('renders "Not downloaded" when model is not available', async () => {
    mockFetch((url) => {
      if (url === '/api/whisper/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse()),
        });
      }
      return Promise.resolve({ ok: false });
    });

    render(<WhisperManager />);

    await waitFor(() => {
      expect(screen.getByText('Not downloaded')).toBeInTheDocument();
    });
  });

  it('renders "Available" when model exists', async () => {
    mockFetch((url) => {
      if (url === '/api/whisper/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse({ available: true, state: 'ready', progress: 100 })),
        });
      }
      return Promise.resolve({ ok: false });
    });

    render(<WhisperManager />);

    await waitFor(() => {
      expect(screen.getByText('Available')).toBeInTheDocument();
    });
  });

  it('shows Download button when not available', async () => {
    mockFetch((url) => {
      if (url === '/api/whisper/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse()),
        });
      }
      return Promise.resolve({ ok: false });
    });

    render(<WhisperManager />);

    await waitFor(() => {
      expect(screen.getByText('Download Model')).toBeInTheDocument();
    });
  });

  it('shows Remove button when available', async () => {
    mockFetch((url) => {
      if (url === '/api/whisper/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse({ available: true, state: 'ready', progress: 100 })),
        });
      }
      return Promise.resolve({ ok: false });
    });

    render(<WhisperManager />);

    await waitFor(() => {
      expect(screen.getByText('Remove Model')).toBeInTheDocument();
    });
  });

  it('shows error state with Retry button', async () => {
    mockFetch((url) => {
      if (url === '/api/whisper/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse({ state: 'error', error: 'network timeout' })),
        });
      }
      return Promise.resolve({ ok: false });
    });

    render(<WhisperManager />);

    await waitFor(() => {
      expect(screen.getByText('network timeout')).toBeInTheDocument();
      expect(screen.getByText('Retry Download')).toBeInTheDocument();
    });
  });

  it('shows downloading state when server reports downloading', async () => {
    mockFetch((url) => {
      if (url === '/api/whisper/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse({ state: 'downloading', progress: 42 })),
        });
      }
      return Promise.resolve({ ok: false });
    });

    render(<WhisperManager />);

    await waitFor(() => {
      expect(screen.getByText('Downloading...')).toBeInTheDocument();
    });
  });

  it('triggers download on button click', async () => {
    let postCalled = false;
    mockFetch((url, opts) => {
      if (url === '/api/whisper/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse()),
        });
      }
      if (url === '/api/whisper/download' && opts?.method === 'POST') {
        postCalled = true;
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ status: 'downloading' }),
        });
      }
      if (url === '/api/whisper/download/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse({ available: true, state: 'ready', progress: 100 })),
        });
      }
      return Promise.resolve({ ok: false });
    });

    render(<WhisperManager />);

    await waitFor(() => {
      expect(screen.getByText('Download Model')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Download Model'));

    await waitFor(() => {
      expect(postCalled).toBe(true);
    });
  });

  it('triggers remove on button click', async () => {
    let deleteCalled = false;
    mockFetch((url, opts) => {
      if (url === '/api/whisper/status') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(statusResponse({ available: true, state: 'ready', progress: 100 })),
        });
      }
      if (url === '/api/whisper/remove' && opts?.method === 'DELETE') {
        deleteCalled = true;
        return Promise.resolve({ ok: true });
      }
      return Promise.resolve({ ok: false });
    });

    render(<WhisperManager />);

    await waitFor(() => {
      expect(screen.getByText('Remove Model')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Remove Model'));

    await waitFor(() => {
      expect(deleteCalled).toBe(true);
    });
  });
});
