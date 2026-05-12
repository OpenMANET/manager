// =============================================================================
// websocketService.test.js — Tests for WebSocket connection management
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { MSG_TYPE } from '../constants.js';

// We need to re-import the module fresh for each test group because
// the module holds state (ws, reconnectTimer). Use dynamic import
// after resetting mocks.

let mockWsInstances;

class MockWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  constructor(url) {
    this.url = url;
    this.readyState = MockWebSocket.CONNECTING;
    this.binaryType = '';
    this.onopen = null;
    this.onclose = null;
    this.onerror = null;
    this.onmessage = null;
    this.send = vi.fn();
    this.close = vi.fn();
    mockWsInstances.push(this);
  }
}

// Make static constants accessible on instances too
MockWebSocket.prototype.CONNECTING = 0;
MockWebSocket.prototype.OPEN = 1;

beforeEach(() => {
  mockWsInstances = [];
  vi.stubGlobal('WebSocket', MockWebSocket);
  vi.stubGlobal('location', { protocol: 'https:', host: 'example.com' });
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.resetModules();
});

describe('TestConnect', () => {
  it('creates WebSocket with correct URL for https', async () => {
    const ws = await import('../services/websocketService.js');
    ws.connect();
    expect(mockWsInstances).toHaveLength(1);
    expect(mockWsInstances[0].url).toBe('wss://example.com/ws');
    expect(mockWsInstances[0].binaryType).toBe('arraybuffer');
  });

  it('creates WebSocket with ws: for http', async () => {
    vi.stubGlobal('location', { protocol: 'http:', host: 'localhost:8080' });
    const ws = await import('../services/websocketService.js');
    ws.connect();
    expect(mockWsInstances[0].url).toBe('ws://localhost:8080/ws');
  });
});

describe('TestIsOpen', () => {
  it('returns falsy when not connected', async () => {
    const ws = await import('../services/websocketService.js');
    expect(ws.isOpen()).toBeFalsy();
  });

  it('returns true when connected', async () => {
    const ws = await import('../services/websocketService.js');
    ws.connect();
    mockWsInstances[0].readyState = MockWebSocket.OPEN;
    expect(ws.isOpen()).toBe(true);
  });
});

describe('TestSendToggle', () => {
  it('sends correct binary format [type, ch, on]', async () => {
    const ws = await import('../services/websocketService.js');
    ws.connect();
    mockWsInstances[0].readyState = MockWebSocket.OPEN;

    ws.sendToggle(MSG_TYPE.RX_TOGGLE, 3, true);

    expect(mockWsInstances[0].send).toHaveBeenCalledTimes(1);
    const sent = new Uint8Array(mockWsInstances[0].send.mock.calls[0][0]);
    expect(sent[0]).toBe(MSG_TYPE.RX_TOGGLE);
    expect(sent[1]).toBe(3);
    expect(sent[2]).toBe(1);
  });

  it('sends 0 for off', async () => {
    const ws = await import('../services/websocketService.js');
    ws.connect();
    mockWsInstances[0].readyState = MockWebSocket.OPEN;

    ws.sendToggle(MSG_TYPE.TX_TOGGLE, 2, false);

    const sent = new Uint8Array(mockWsInstances[0].send.mock.calls[0][0]);
    expect(sent[2]).toBe(0);
  });
});

describe('TestSendByte', () => {
  it('sends single byte', async () => {
    const ws = await import('../services/websocketService.js');
    ws.connect();
    mockWsInstances[0].readyState = MockWebSocket.OPEN;

    ws.sendByte(MSG_TYPE.PTT_DOWN);

    const sent = new Uint8Array(mockWsInstances[0].send.mock.calls[0][0]);
    expect(sent.length).toBe(1);
    expect(sent[0]).toBe(MSG_TYPE.PTT_DOWN);
  });
});

describe('TestSendWhenNotOpen', () => {
  it('does nothing when ws is not open', async () => {
    const ws = await import('../services/websocketService.js');
    // No connection at all
    ws.send(new Uint8Array([1, 2, 3]).buffer);
    // Should not throw

    // Connected but not OPEN
    ws.connect();
    mockWsInstances[0].readyState = MockWebSocket.CONNECTING;
    ws.send(new Uint8Array([1, 2, 3]).buffer);
    expect(mockWsInstances[0].send).not.toHaveBeenCalled();
  });
});

describe('TestStatusCallback', () => {
  it('fires connecting on connect, connected on open', async () => {
    const ws = await import('../services/websocketService.js');
    const statusFn = vi.fn();
    ws.setCallbacks({ statusChange: statusFn });

    ws.connect();
    expect(statusFn).toHaveBeenCalledWith('connecting');

    // Simulate open
    mockWsInstances[0].onopen();
    expect(statusFn).toHaveBeenCalledWith('connected');
  });

  it('fires disconnected on close', async () => {
    const ws = await import('../services/websocketService.js');
    const statusFn = vi.fn();
    ws.setCallbacks({ statusChange: statusFn });

    ws.connect();
    mockWsInstances[0].onopen();

    mockWsInstances[0].onclose();
    expect(statusFn).toHaveBeenCalledWith('disconnected');
  });
});

describe('TestRxAudioCallback', () => {
  it('parses ch, IP, opus data from binary message', async () => {
    const ws = await import('../services/websocketService.js');
    const rxFn = vi.fn();
    ws.setCallbacks({ rxAudio: rxFn });

    ws.connect();
    mockWsInstances[0].readyState = MockWebSocket.OPEN;

    // Build an RX_AUDIO message:
    // [0x01][ch=2][ssrc(4)][seq(2)][ip 10.0.1.5][opus payload 0xAA 0xBB]
    const msg = new Uint8Array([
      0x01,       // type
      2,          // channel
      0, 0, 0, 1, // SSRC
      0, 42,      // sequence
      10, 0, 1, 5, // source IP
      0xaa, 0xbb, // opus payload
    ]);

    mockWsInstances[0].onmessage({ data: msg.buffer });

    expect(rxFn).toHaveBeenCalledTimes(1);
    const [ch, srcIP, opusData] = rxFn.mock.calls[0];
    expect(ch).toBe(2);
    expect(srcIP).toBe('10.0.1.5');
    expect(opusData[0]).toBe(0xaa);
    expect(opusData[1]).toBe(0xbb);
  });

  it('ignores non-ArrayBuffer messages', async () => {
    const ws = await import('../services/websocketService.js');
    const rxFn = vi.fn();
    ws.setCallbacks({ rxAudio: rxFn });

    ws.connect();
    mockWsInstances[0].onmessage({ data: 'text message' });
    expect(rxFn).not.toHaveBeenCalled();
  });
});

describe('TestReconnect', () => {
  it('schedules reconnect after disconnect', async () => {
    const ws = await import('../services/websocketService.js');
    ws.connect();
    const firstWs = mockWsInstances[0];

    // Simulate close — set readyState to CLOSED so connect() creates a new one
    firstWs.readyState = MockWebSocket.CLOSED;
    firstWs.onclose();

    // Advance past reconnect delay (2000ms)
    vi.advanceTimersByTime(2000);

    // A new WebSocket should have been created
    expect(mockWsInstances.length).toBe(2);
  });
});
