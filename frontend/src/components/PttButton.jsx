// =============================================================================
// PttButton.jsx — Push-to-talk button with TX channel info
// =============================================================================
// Large circular button with mic icon. Active state shows red gradient.
// Mouse and touch events are handled here; keyboard (Space) is in App.jsx.
//
// Props:
//   active     — boolean, whether PTT is currently active
//   onDown     — function, called when PTT pressed
//   onUp       — function, called when PTT released
//   txChannels — array of active TX channel numbers (for display)

import React, { useRef, useEffect } from 'react';

export default function PttButton({ active, onDown, onUp, txChannels, channelAliases, voxEnabled, onVoxToggle }) {
  const btnRef = useRef(null);

  useEffect(() => {
    const btn = btnRef.current;
    if (!btn) return;

    const handleTouchStart = (e) => { e.preventDefault(); onDown(); };
    const handleTouchEnd = (e) => { e.preventDefault(); onUp(); };
    const handleTouchCancel = () => onUp();

    btn.addEventListener('touchstart', handleTouchStart, { passive: false });
    btn.addEventListener('touchend', handleTouchEnd, { passive: false });
    btn.addEventListener('touchcancel', handleTouchCancel);

    return () => {
      btn.removeEventListener('touchstart', handleTouchStart);
      btn.removeEventListener('touchend', handleTouchEnd);
      btn.removeEventListener('touchcancel', handleTouchCancel);
    };
  }, [onDown, onUp]);
  // Build the TX info string shown below the button.
  let txInfoContent;
  if (!txChannels || txChannels.length === 0) {
    txInfoContent = <span>TX: <span style={{ color: 'var(--crit)' }}>none</span></span>;
  } else if (txChannels.length === 5) {
    txInfoContent = <span>TX: <span className="active-ch">All channels</span></span>;
  } else {
    const labels = txChannels.map((ch) => (channelAliases && channelAliases[ch]) || `Ch ${ch}`);
    txInfoContent = <span>TX: <span className="active-ch">{labels.join(', ')}</span></span>;
  }

  return (
    <div className="card ptt-container">
      <button
        ref={btnRef}
        className={`ptt-btn${active ? ' active' : ''}`}
        onMouseDown={(e) => { e.preventDefault(); onDown(); }}
        onMouseUp={(e) => { e.preventDefault(); onUp(); }}
        onMouseLeave={() => onUp()}
      >
        <div className="icon">{'\u{1F399}'}</div>
        <div>PTT</div>
        <div className="sublabel">Hold to talk</div>
      </button>
      <div className="tx-info">{txInfoContent}</div>
      <label className="vox-toggle">
        <input
          type="checkbox"
          checked={voxEnabled || false}
          onChange={(e) => onVoxToggle && onVoxToggle(e.target.checked)}
        />
        VOX
      </label>
    </div>
  );
}
