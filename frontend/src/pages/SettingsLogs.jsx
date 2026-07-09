// =============================================================================
// SettingsLogs.jsx — OpenWRT system log + kernel dmesg viewer
// =============================================================================
//
// Fetches from LogsService.GetLogs. Operator UX:
//  - segmented [logread | dmesg] toggle in the toolbar
//  - selectable auto-refresh cadence (persisted in localStorage)
//  - manual refresh, scroll-lock, and download buttons
//  - auto-scroll to bottom unless the user has scrolled up

import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { createClient, Code, ConnectError } from '@connectrpc/connect';
import { transport } from '../services/connectClient.js';
import { useVisibleInterval } from '../hooks/useVisibleInterval.js';
import { LogsService } from '../gen/openmanet/logs/v1/logs_service_pb.js';
import { LogSource } from '../gen/openmanet/logs/v1/logs_service_pb.js';
import LatSelect from '../components/LatSelect.jsx';
import './SettingsLogs.css';

const logsClient = createClient(LogsService, transport);

const MAX_LINES = 1000;

// Interval options in milliseconds. 0 means "off".
const INTERVAL_OPTIONS = [
  { value: 0, label: 'Off' },
  { value: 2000, label: '2s' },
  { value: 5000, label: '5s' },
  { value: 10000, label: '10s' },
  { value: 30000, label: '30s' },
  { value: 60000, label: '60s' },
];

const STORAGE_KEY_INTERVAL = 'openmanetd.logs.intervalMs';
const DEFAULT_INTERVAL_MS = 10000;
const SCROLL_TOLERANCE_PX = 24;

const SOURCES = [
  { id: 'logread', label: 'logread', proto: LogSource.LOGREAD },
  { id: 'dmesg', label: 'dmesg', proto: LogSource.DMESG },
];

function loadInitialInterval() {
  if (typeof window === 'undefined') return DEFAULT_INTERVAL_MS;
  const raw = window.localStorage?.getItem(STORAGE_KEY_INTERVAL);
  if (raw == null) return DEFAULT_INTERVAL_MS;
  const n = Number(raw);
  if (!Number.isFinite(n) || n < 0) return DEFAULT_INTERVAL_MS;
  return INTERVAL_OPTIONS.some((o) => o.value === n) ? n : DEFAULT_INTERVAL_MS;
}

function formatTimestamp(ts) {
  if (!ts) return '—';
  // Connect-JS returns google.protobuf.Timestamp as a Timestamp object with
  // seconds/nanos fields; toDate() exists on newer runtimes, but we defensively
  // support both shapes.
  let d;
  if (typeof ts.toDate === 'function') {
    d = ts.toDate();
  } else if (ts.seconds != null) {
    const seconds = typeof ts.seconds === 'bigint' ? Number(ts.seconds) : Number(ts.seconds);
    const nanos = Number(ts.nanos || 0);
    d = new Date(seconds * 1000 + Math.floor(nanos / 1e6));
  } else {
    return '—';
  }
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

// stripAnsi removes ANSI/VT100 escape sequences (CSI + final byte) from
// a string. zerolog's ConsoleWriter colourises output with sequences like
// ESC[32m … ESC[0m; when those reach syslog they survive into logread
// and would render as literal "[32m" in a plain <pre>. We strip them
// rather than render them — adding a full ANSI-to-HTML pass is overkill
// for an operator log viewer.
// eslint-disable-next-line no-control-regex
const ANSI_RE = /\x1b\[[0-9;?]*[A-Za-z]/g;
function stripAnsi(s) {
  if (!s) return s;
  return s.replace(ANSI_RE, '');
}

function filenameTimestamp(date = new Date()) {
  const pad = (n) => String(n).padStart(2, '0');
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}-` +
    `${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  );
}

export default function SettingsLogs() {
  const [sourceId, setSourceId] = useState('logread');
  const [intervalMs, setIntervalMs] = useState(loadInitialInterval);
  const [lines, setLines] = useState([]);
  const [collectedAt, setCollectedAt] = useState(null);
  const [truncated, setTruncated] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [manualLock, setManualLock] = useState(false);

  // atBottomRef tracks whether the user is pinned to the bottom of the
  // scroll region. When true, new content auto-scrolls into view. When the
  // user scrolls up we pause auto-scroll so their reading position is not
  // trampled; a "Jump to bottom" chip appears.
  const atBottomRef = useRef(true);
  const [showJumpChip, setShowJumpChip] = useState(false);
  const scrollRef = useRef(null);

  const source = useMemo(
    () => SOURCES.find((s) => s.id === sourceId) ?? SOURCES[0],
    [sourceId],
  );

  // Fetch is wrapped in useRef so the polling hook can call the latest
  // closure without resubscribing on every state change.
  const fetchLogs = useCallback(async () => {
    try {
      const resp = await logsClient.getLogs({
        source: source.proto,
        maxLines: MAX_LINES,
      });
      const nextLines = (resp.lines || []).map((l) => stripAnsi(l.raw));
      setLines(nextLines);
      setCollectedAt(resp.collectedAt || null);
      setTruncated(!!resp.truncated);
      setError(null);
    } catch (e) {
      const isFailedPrecondition =
        e instanceof ConnectError && e.code === Code.FailedPrecondition;
      const message = isFailedPrecondition
        ? `${source.label} is not available on this host.`
        : `Failed to load ${source.label}: ${e?.message || String(e)}`;
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [source.proto, source.label]);

  // Source change: reset bottom-lock and clear content so the user sees a
  // clean slate instead of leftover lines from the other source. Could be
  // expressed via a `key` reset in the parent, but doing it locally keeps
  // SettingsLogs.jsx self-contained.
  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    atBottomRef.current = true;
    setShowJumpChip(false);
    setLines([]);
    setCollectedAt(null);
    setTruncated(false);
    setLoading(true);
    // fetchLogs runs via useVisibleInterval's mount-fire below.
  }, [sourceId]);
  /* eslint-enable react-hooks/set-state-in-effect */

  // Poll with visibility-pause. intervalMs=0 disables polling; we still
  // run an initial fetch manually so the view is populated on load.
  useVisibleInterval(fetchLogs, intervalMs, [fetchLogs]);

  useEffect(() => {
    if (intervalMs === 0) {
      // Polling is off; trigger an initial load on mount and whenever the
      // source changes (fetchLogs identity captures source). fetchLogs sets
      // local state with the response — an effect is the right home.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      fetchLogs();
    }
  }, [intervalMs, fetchLogs]);

  // Auto-scroll to bottom when new content arrives, unless the user has
  // scrolled up or explicitly engaged the scroll-lock.
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (manualLock) return;
    if (!atBottomRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [lines, manualLock]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceFromBottom < SCROLL_TOLERANCE_PX;
    atBottomRef.current = atBottom;
    setShowJumpChip(!atBottom);
  }, []);

  const handleJumpToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    atBottomRef.current = true;
    setShowJumpChip(false);
    el.scrollTop = el.scrollHeight;
  }, []);

  const handleIntervalChange = useCallback((next) => {
    setIntervalMs(next);
    try {
      window.localStorage?.setItem(STORAGE_KEY_INTERVAL, String(next));
    } catch {
      // Quota or disabled storage — ignore; cadence still applies per session.
    }
  }, []);

  const handleSourceChange = useCallback((id) => {
    setSourceId(id);
  }, []);

  const handleManualRefresh = useCallback(() => {
    // Explicit refresh clears lock and re-anchors to bottom.
    atBottomRef.current = true;
    setShowJumpChip(false);
    fetchLogs();
  }, [fetchLogs]);

  const handleToggleLock = useCallback(() => {
    setManualLock((v) => !v);
  }, []);

  const handleDownload = useCallback(() => {
    if (!lines.length) return;
    const body = lines.join('\n') + '\n';
    const blob = new Blob([body], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    try {
      const a = document.createElement('a');
      a.href = url;
      a.download = `${source.id}-${filenameTimestamp()}.log`;
      document.body.appendChild(a);
      a.click();
      a.remove();
    } finally {
      URL.revokeObjectURL(url);
    }
  }, [lines, source.id]);

  return (
    <div className="settings-logs">
      <div className="settings-logs-toolbar">
        <div className="settings-logs-source" role="tablist" aria-label="Log source">
          {SOURCES.map((s) => (
            <button
              key={s.id}
              type="button"
              role="tab"
              aria-selected={sourceId === s.id}
              className={`lat-btn${sourceId === s.id ? ' primary' : ''}`}
              onClick={() => handleSourceChange(s.id)}
            >
              {s.label}
            </button>
          ))}
        </div>

        <div className="settings-logs-controls">
          <div className="settings-logs-field">
            <span className="settings-logs-label">Refresh</span>
            <LatSelect
              ariaLabel="Auto-refresh interval"
              value={intervalMs}
              onChange={handleIntervalChange}
              options={INTERVAL_OPTIONS}
            />
          </div>
          <button
            type="button"
            className="lat-btn"
            onClick={handleManualRefresh}
            title="Refresh now"
            aria-label="Refresh now"
          >
            Refresh
          </button>
          <button
            type="button"
            className={`lat-btn${manualLock ? ' primary' : ''}`}
            onClick={handleToggleLock}
            title={manualLock ? 'Auto-scroll paused' : 'Auto-scroll on'}
            aria-pressed={manualLock}
          >
            {manualLock ? 'Locked' : 'Auto-scroll'}
          </button>
          <button
            type="button"
            className="lat-btn"
            onClick={handleDownload}
            disabled={!lines.length}
            title="Download current log as .log file"
          >
            Download
          </button>
        </div>
      </div>

      <div className="settings-logs-meta">
        <span>
          Collected {formatTimestamp(collectedAt)}
          {truncated ? ` • Showing last ${MAX_LINES} lines (older truncated)` : ''}
          {loading && !lines.length ? ' • Loading…' : ''}
        </span>
      </div>

      {error && <div className="settings-banner crit settings-logs-banner">{error}</div>}

      <div className="settings-logs-host">
        <pre
          ref={scrollRef}
          onScroll={handleScroll}
          className="settings-logs-pre"
          aria-live={manualLock ? 'off' : 'polite'}
          aria-busy={loading}
          aria-label={`${source.label} output`}
        >
          {lines.length === 0 && !loading && !error
            ? `No ${source.label} entries to display.`
            : lines.join('\n')}
        </pre>
        {showJumpChip && (
          <button
            type="button"
            className="lat-btn primary settings-logs-jumpchip"
            onClick={handleJumpToBottom}
          >
            ↓ Jump to bottom
          </button>
        )}
      </div>
    </div>
  );
}
