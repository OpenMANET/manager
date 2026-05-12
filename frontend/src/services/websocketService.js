// =============================================================================
// websocketService.js — WebSocket connection management
// =============================================================================
//
// Manages the WebSocket connection to the Go backend (openmanetd).
// Handles automatic reconnection and binary protocol framing.
//
// PROTOCOL
// All messages are binary (ArrayBuffer).  The first byte identifies the
// message type (see MSG_TYPE in constants.js).  The only server->client
// message type currently is RX_AUDIO (0x01):
//
//   Byte 0:    0x01 (message type)
//   Byte 1:    channel number (1-5)
//   Bytes 2-5: SSRC (4 bytes, not used by the UI but forwarded by the backend)
//   Bytes 6-7: sequence number (2 bytes, big-endian)
//   Bytes 8-11: source IP address (4 bytes, one per octet)
//   Bytes 12+: Opus-encoded audio payload
//
// Client->server messages are simpler:
//   TX_AUDIO:  [0x02][opus payload...]
//   TOGGLE:    [type byte][channel][0x00 or 0x01]
//   PTT/ALL:   [single type byte]

import { MSG_TYPE } from '../constants.js';

// -----------------------------------------------------------------------------
// Module state
// -----------------------------------------------------------------------------

let ws = null;
let reconnectTimer = null;

// Callbacks set by the consumer (typically the App component).
// Using a callback pattern rather than events keeps the API simple and avoids
// the need for an EventEmitter polyfill.
let onStatusChange = null; // (status: 'connected'|'connecting'|'disconnected') => void
let onRxAudio = null;      // (ch, srcIP, opusData) => void
let onError = null;        // (msg) => void
let onLog = null;          // (msg, cls) => void

// -----------------------------------------------------------------------------
// setCallbacks({ statusChange, rxAudio, error, log })
// -----------------------------------------------------------------------------
// Registers callback functions.  Must be called before connect().
// All callbacks are optional — if not provided, the corresponding events are
// silently ignored.
export function setCallbacks({ statusChange, rxAudio, error, log }) {
  onStatusChange = statusChange || null;
  onRxAudio = rxAudio || null;
  onError = error || null;
  onLog = log || null;
}

// -----------------------------------------------------------------------------
// connect()
// -----------------------------------------------------------------------------
// Opens a WebSocket to the same host that served the page, using wss: for
// HTTPS and ws: for HTTP.  The backend expects the path "/ws".
//
// If a connection is already open or connecting, this is a no-op.
// On successful open, notifies the consumer via statusChange('connected')
// so it can re-sync channel enable/disable state.
export function connect() {
  // Don't open a second connection if one is already alive or connecting.
  if (ws && ws.readyState <= WebSocket.OPEN) {
    return;
  }

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${proto}//${location.host}/ws`);
  ws.binaryType = 'arraybuffer';

  if (onStatusChange) onStatusChange('connecting');

  ws.onopen = () => {
    if (onStatusChange) onStatusChange('connected');
    if (onLog) onLog('Connected', 'info');

    // Clear any pending reconnect timer — we're connected now.
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }

    // The consumer should re-send channel states here.  We don't do it in
    // this module because channel state lives in the App component.
  };

  ws.onclose = () => {
    if (onStatusChange) onStatusChange('disconnected');
    scheduleReconnect();
  };

  ws.onerror = () => {
    if (onError) onError('WebSocket error');
    if (onLog) onLog('WebSocket error', 'err');
  };

  ws.onmessage = (evt) => {
    if (!(evt.data instanceof ArrayBuffer)) return;

    const v = new Uint8Array(evt.data);

    // Parse RX_AUDIO messages.
    // Layout: [0x01][ch][ssrc(4)][seq(2)][srcIP(4)][opus...]
    // Minimum length: 1 + 1 + 4 + 2 + 4 + 1 = 13 bytes (at least 1 byte of opus)
    if (v[0] === MSG_TYPE.RX_AUDIO && v.length > 12) {
      const ch = v[1];
      // Extract source IP from bytes 8-11 as a dotted-quad string.
      const srcIP = `${v[8]}.${v[9]}.${v[10]}.${v[11]}`;
      // Opus payload starts at byte 12.
      const opusData = v.slice(12);

      if (onRxAudio) onRxAudio(ch, srcIP, opusData);
    }
  };
}

// -----------------------------------------------------------------------------
// scheduleReconnect()
// -----------------------------------------------------------------------------
// Schedules a reconnection attempt after 2 seconds.  Only one timer is active
// at a time.  2 seconds is long enough to avoid hammering a down server but
// short enough that the user doesn't wait long.
function scheduleReconnect() {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, 2000);
}

// -----------------------------------------------------------------------------
// disconnect()
// -----------------------------------------------------------------------------
// Closes the WebSocket connection and cancels any pending reconnect timer.
// Safe to call even if not connected.
export function disconnect() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  if (ws) {
    // Remove handlers to prevent triggering reconnect on intentional close.
    ws.onclose = null;
    ws.onerror = null;
    if (ws.readyState <= WebSocket.OPEN) {
      ws.close();
    }
    ws = null;
  }
}

// -----------------------------------------------------------------------------
// send(data)
// -----------------------------------------------------------------------------
// Sends an ArrayBuffer over the WebSocket if the connection is open.
// Silently drops the data if the socket isn't connected — this matches the
// original behavior where TX data during a disconnect is simply lost.
export function send(data) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(data);
  }
}

// -----------------------------------------------------------------------------
// isOpen()
// -----------------------------------------------------------------------------
// Returns true if the WebSocket is currently connected and ready to send.
export function isOpen() {
  return ws && ws.readyState === WebSocket.OPEN;
}

// -----------------------------------------------------------------------------
// sendToggle(type, ch, on)
// -----------------------------------------------------------------------------
// Sends a 3-byte toggle message: [type][channel][0x00 or 0x01].
// Used for RX_TOGGLE (0x04) and TX_TOGGLE (0x03).
export function sendToggle(type, ch, on) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(new Uint8Array([type, ch, on ? 1 : 0]).buffer);
  }
}

// -----------------------------------------------------------------------------
// sendByte(byte)
// -----------------------------------------------------------------------------
// Sends a single-byte command.  Used for:
//   - PTT_DOWN  (0x09)
//   - PTT_UP    (0x0A)
//   - RX_ALL_ON (0x05), RX_ALL_OFF (0x06)
//   - TX_ALL_ON (0x07), TX_ALL_OFF (0x08)
export function sendByte(byte) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(new Uint8Array([byte]).buffer);
  }
}
