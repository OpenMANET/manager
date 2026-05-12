// =============================================================================
// LogBox.jsx — System log display
// =============================================================================
// Monospace log output with color-coded entries by class.
// Classes: rx (green), tx (orange), err (red), info (accent).
//
// Props:
//   logs — array of {msg, cls} where msg includes timestamp prefix

import React, { useRef, useEffect } from 'react';

export default React.memo(function LogBox({ logs }) {
  const logRef = useRef(null);

  // Auto-scroll to bottom when new log entries arrive.
  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <div className="card span-full">
      <div className="log-box" ref={logRef}>
        {logs.map((entry, i) => (
          <div key={i} className={entry.cls || ''}>
            {entry.msg}
          </div>
        ))}
      </div>
    </div>
  );
});
