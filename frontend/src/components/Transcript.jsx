// =============================================================================
// Transcript.jsx — Chat transcript with whisper captions
// =============================================================================
// Scrollable message list with channel filter, whisper controls, and debug mode.
// Messages are colored by channel with timestamp, channel badge, IP, and text.
//
// Props:
//   messages        — array of {ch, ip, text, ts}
//   whisperEnabled  — boolean
//   onWhisperToggle — function(enabled)
//   debugMode       — boolean
//   onDebugToggle   — function(enabled)
//   whisperStatus   — string to display below chat
//   activeFilter    — number (0 = all, 1-5 = specific channel)
//   onFilterChange  — function(filterValue)

import React, { useRef, useEffect } from 'react';
import LatSelect from './LatSelect.jsx';

function defaultSeverity(text) {
  if (!text) return null;
  const t = text.toLowerCase();
  if (/(losing|lost|dropped|dropping|critical|down|offline)/.test(t)) return 'crit';
  if (/(battery|degraded|warn|heads up|reconverging|reconnect)/.test(t)) return 'warn';
  return null;
}

export default function Transcript({
  messages,
  whisperEnabled,
  onWhisperToggle,
  debugMode,
  onDebugToggle,
  whisperStatus,
  activeFilter,
  onFilterChange,
  channelAliases,
  compact = false,
  severityOf = defaultSeverity,
}) {
  const chatBoxRef = useRef(null);

  // Auto-scroll to bottom when new messages arrive.
  useEffect(() => {
    if (chatBoxRef.current) {
      chatBoxRef.current.scrollTop = chatBoxRef.current.scrollHeight;
    }
  }, [messages]);

  return (
    <div className={compact ? '' : 'card span-full'}>
      {/* Header with title and channel filter (hidden in compact mode) */}
      {!compact && (
        <>
          <div className="chat-header">
            <div className="card-title" style={{ marginBottom: 0 }}>Transcript</div>
            <div className="chat-filter">
              <LatSelect
                ariaLabel="Channel filter"
                value={activeFilter}
                onChange={(v) => onFilterChange(v)}
                options={[
                  { value: 0, label: 'All Channels' },
                  ...[1, 2, 3, 4, 5].map((ch) => ({
                    value: ch,
                    label: (channelAliases && channelAliases[ch]) || `Ch ${ch}`,
                  })),
                ]}
              />
            </div>
          </div>

          {/* Whisper and debug controls */}
          <div className="whisper-ctrl">
            <input
              type="checkbox"
              id="cc-toggle"
              checked={whisperEnabled}
              onChange={(e) => onWhisperToggle(e.target.checked)}
            />
            <label htmlFor="cc-toggle">Enable closed captions (offline Whisper AI)</label>
            <input
              type="checkbox"
              id="debug-toggle"
              checked={debugMode}
              onChange={(e) => onDebugToggle(e.target.checked)}
            />
            <label htmlFor="debug-toggle" style={{ color: 'var(--orange)' }}>Debug</label>
          </div>
        </>
      )}

      {/* Message list */}
      <div className={compact ? 'tr-box' : 'chat-box'} ref={chatBoxRef}>
        {messages.map((msg, i) => {
          const hidden = !compact && activeFilter !== 0 && msg.ch !== activeFilter;
          const sev = severityOf ? severityOf(msg.text) : null;
          if (compact) {
            return (
              <div
                key={i}
                className={`tr-row${sev ? ' ' + sev : ''}`}
                data-ch={msg.ch}
              >
                <span className="tr-ts">{msg.ts}</span>
                <span className="tr-src">{msg.ip || (channelAliases && channelAliases[msg.ch]) || `CH ${msg.ch}`}</span>
                <span className="tr-text">{msg.text}</span>
              </div>
            );
          }
          return (
            <div
              key={i}
              className={`chat-msg${hidden ? ' hidden' : ''}${sev ? ' ' + sev : ''}`}
              data-ch={msg.ch}
            >
              <span className="chat-ts">{msg.ts}</span>
              <span className={`chat-ch chat-ch-${msg.ch}`}>Ch{msg.ch}</span>
              <span className="chat-ip">{msg.ip}</span>
              <span className="chat-text">{msg.text}</span>
            </div>
          );
        })}
      </div>

      {/* Whisper status line (hidden in compact mode) */}
      {!compact && <div className="whisper-status">{whisperStatus}</div>}
    </div>
  );
}
