// =============================================================================
// ChannelGrid.jsx — Channel RX/TX toggle grid with bulk controls
// =============================================================================
// Renders the channel grid showing each channel's label, port, RX/TX buttons,
// replay button, and a green RX-dot that pulses when audio is recently received.

import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useVisibleInterval } from '../hooks/useVisibleInterval.js';

export default function ChannelGrid({
  channels,
  rxEnabled,
  txEnabled,
  rxLastTimeRef,
  onToggleRx,
  onToggleTx,
  onRxAll,
  onTxAll,
  channelAliases,
  onAliasChange,
  onReplay,
  replayAvailable,
  tiles = false,
}) {
  // Force re-render at 150ms interval to update RX dot activity state.
  // Visibility-gated so a backgrounded tab does no work.
  const [, setTick] = useState(0);
  const [editingCh, setEditingCh] = useState(null);
  const inputRef = useRef(null);

  const tickRxDots = useCallback(() => setTick((t) => t + 1), []);
  useVisibleInterval(tickRxDots, 150);

  useEffect(() => {
    if (editingCh !== null && inputRef.current) {
      inputRef.current.focus();
      inputRef.current.select();
    }
  }, [editingCh]);

  const getLabel = (ch) => (channelAliases && channelAliases[ch]) || `Ch ${ch}`;

  const commitAlias = (ch, value) => {
    if (onAliasChange) onAliasChange(ch, value);
    setEditingCh(null);
  };

  if (tiles) {
    return (
      <div className="lat-panel ch-tiles-panel">
        <div className="panel-head"><h3>Channels</h3></div>
        <div className="ch-tiles-grid">
          {channels.map((c) => {
            const rxLast = rxLastTimeRef.current;
            const rxActive = rxLast[c.ch] && Date.now() - rxLast[c.ch] < 500;
            const active = rxEnabled[c.ch] || txEnabled[c.ch];
            return (
              <div key={c.ch} className={`ch-tile${active ? ' active' : ''}`}>
                <div className="ch-tile-head">
                  <span className={`ch-tile-dot${rxActive ? ' active' : ''}`} />
                  <span className="ch-tile-num">CH {c.ch}</span>
                  {editingCh === c.ch ? (
                    <input
                      ref={inputRef}
                      className="ch-alias-input"
                      defaultValue={getLabel(c.ch)}
                      onBlur={(e) => commitAlias(c.ch, e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') commitAlias(c.ch, e.target.value);
                        if (e.key === 'Escape') setEditingCh(null);
                      }}
                      maxLength={16}
                    />
                  ) : (
                    <span
                      className="ch-tile-alias"
                      onDoubleClick={() => setEditingCh(c.ch)}
                      title="Double-click to rename"
                    >
                      {getLabel(c.ch).toUpperCase()}
                    </span>
                  )}
                </div>
                <div className="ch-tile-pills">
                  <button
                    className={`ch-pill${rxEnabled[c.ch] ? ' rx-on' : ''}`}
                    onClick={() => onToggleRx(c.ch)}
                  >RX</button>
                  <button
                    className={`ch-pill${txEnabled[c.ch] ? ' tx-on' : ''}`}
                    onClick={() => onToggleTx(c.ch)}
                  >TX</button>
                  <button
                    className="ch-pill ghost"
                    onClick={() => onReplay && onReplay(c.ch)}
                    disabled={!replayAvailable || !replayAvailable[c.ch]}
                    title="Replay last received audio"
                  >&#9654;</button>
                </div>
              </div>
            );
          })}
        </div>
        <div className="ch-all-row">
          <button className="lat-btn ghost" onClick={() => onRxAll(true)}>RX ALL</button>
          <button className="lat-btn ghost" onClick={() => onRxAll(false)}>RX NONE</button>
          <button className="lat-btn ghost" onClick={() => onTxAll(true)}>TX ALL</button>
          <button className="lat-btn ghost" onClick={() => onTxAll(false)}>TX NONE</button>
        </div>
      </div>
    );
  }

  return (
    <div className="card">
      <div className="card-title">Channels</div>
      <div className="ch-grid">
        {channels.map((c) => {
          const rxLast = rxLastTimeRef.current;
          const rxActive = rxLast[c.ch] && Date.now() - rxLast[c.ch] < 500;
          return (
            <React.Fragment key={c.ch}>
              {/* Channel label with RX activity dot — double-click to edit */}
              <div className="ch-label">
                <span className={`ch-rx-dot${rxActive ? ' active' : ''}`} />
                {editingCh === c.ch ? (
                  <input
                    ref={inputRef}
                    className="ch-alias-input"
                    defaultValue={getLabel(c.ch)}
                    onBlur={(e) => commitAlias(c.ch, e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') commitAlias(c.ch, e.target.value);
                      if (e.key === 'Escape') setEditingCh(null);
                    }}
                    maxLength={16}
                  />
                ) : (
                  <span
                    onDoubleClick={() => setEditingCh(c.ch)}
                    title="Double-click to rename"
                    style={{ cursor: 'default' }}
                  >
                    {getLabel(c.ch)}
                  </span>
                )}
              </div>

              {/* Port number */}
              <div className="ch-port">port {c.port}</div>

              {/* RX toggle button */}
              <button
                className={`ch-btn${rxEnabled[c.ch] ? ' rx-on' : ''}`}
                onClick={() => onToggleRx(c.ch)}
              >
                RX
              </button>

              {/* TX toggle button */}
              <button
                className={`ch-btn${txEnabled[c.ch] ? ' tx-on' : ''}`}
                onClick={() => onToggleTx(c.ch)}
              >
                TX
              </button>

              {/* Replay button */}
              <button
                className="ch-btn ch-replay-btn"
                onClick={() => onReplay && onReplay(c.ch)}
                disabled={!replayAvailable || !replayAvailable[c.ch]}
                title="Replay last received audio"
              >
                &#9654;
              </button>
            </React.Fragment>
          );
        })}
      </div>

      {/* Bulk toggle buttons */}
      <div className="ch-all-row">
        <button className="ch-all-btn" onClick={() => onRxAll(true)}>RX All</button>
        <button className="ch-all-btn" onClick={() => onRxAll(false)}>RX None</button>
        <button className="ch-all-btn tx-style" onClick={() => onTxAll(true)}>TX All</button>
        <button className="ch-all-btn tx-style" onClick={() => onTxAll(false)}>TX None</button>
      </div>
    </div>
  );
}
