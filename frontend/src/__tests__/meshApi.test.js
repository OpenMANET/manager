// =============================================================================
// meshApi.test.js — Tests for mesh network status API
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fetchMeshStatus } from '../services/meshApi.js';

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn());
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('TestFetchMeshStatus', () => {
  it('calls all 4 endpoints', async () => {
    fetch.mockResolvedValue({ json: () => Promise.resolve({}) });

    await fetchMeshStatus();

    expect(fetch).toHaveBeenCalledTimes(4);
    const urls = fetch.mock.calls.map((c) => c[0]);
    expect(urls).toContain('/api/status');
    expect(urls).toContain('/api/nodes');
    expect(urls).toContain('/api/neighbors');
    expect(urls).toContain('/api/interfaces');
  });

  it('returns parsed JSON for each successful endpoint', async () => {
    const mockStatus = { connected: true, neighbors: 3 };
    const mockNodes = [{ hostname: 'node1', ip: '10.0.0.1' }];
    const mockNeighbors = [{ mac: 'aa:bb:cc', signal: -50 }];
    const mockInterfaces = [{ name: 'wlan0', frequency: 5180 }];

    fetch.mockImplementation((url) => {
      const responses = {
        '/api/status': mockStatus,
        '/api/nodes': mockNodes,
        '/api/neighbors': mockNeighbors,
        '/api/interfaces': mockInterfaces,
      };
      return Promise.resolve({ json: () => Promise.resolve(responses[url]) });
    });

    const result = await fetchMeshStatus();

    expect(result.status).toEqual(mockStatus);
    expect(result.nodes).toEqual(mockNodes);
    expect(result.neighbors).toEqual(mockNeighbors);
    expect(result.interfaces).toEqual(mockInterfaces);
  });

  it('returns null for failed endpoints', async () => {
    fetch.mockImplementation((url) => {
      if (url === '/api/status') {
        return Promise.resolve({ json: () => Promise.resolve({ connected: true }) });
      }
      // Simulate network error for other endpoints
      return Promise.reject(new Error('Network error'));
    });

    const result = await fetchMeshStatus();

    expect(result.status).toEqual({ connected: true });
    expect(result.nodes).toBeNull();
    expect(result.neighbors).toBeNull();
    expect(result.interfaces).toBeNull();
  });

  it('handles all endpoints failing gracefully', async () => {
    fetch.mockRejectedValue(new Error('Network down'));

    const result = await fetchMeshStatus();

    expect(result.status).toBeNull();
    expect(result.nodes).toBeNull();
    expect(result.neighbors).toBeNull();
    expect(result.interfaces).toBeNull();
  });
});
