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
}) {
  const chatBoxRef = useRef(null);

  // Auto-scroll to bottom when new messages arrive.
  useEffect(() => {
    if (chatBoxRef.current) {
      chatBoxRef.current.scrollTop = chatBoxRef.current.scrollHeight;
    }
  }, [messages]);

  return (
    <div className="card span-full">
      {/* Header with title and channel filter */}
      <div className="chat-header">
        <div className="card-title" style={{ marginBottom: 0 }}>Transcript</div>
        <div className="chat-filter">
          <select
            value={activeFilter}
            onChange={(e) => onFilterChange(parseInt(e.target.value, 10))}
          >
            <option value="0">All Channels</option>
            {[1,2,3,4,5].map((ch) => (
              <option key={ch} value={ch}>{(channelAliases && channelAliases[ch]) || `Ch ${ch}`}</option>
            ))}
          </select>
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

      {/* Message list */}
      <div className="chat-box" ref={chatBoxRef}>
        {messages.map((msg, i) => {
          const hidden = activeFilter !== 0 && msg.ch !== activeFilter;
          return (
            <div
              key={i}
              className={`chat-msg${hidden ? ' hidden' : ''}`}
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

      {/* Whisper status line */}
      <div className="whisper-status">{whisperStatus}</div>
    </div>
  );
}
