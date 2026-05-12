// =============================================================================
// useVisibleInterval — page-visibility-gated polling
// =============================================================================
//
// Drop-in replacement for the `useEffect(setInterval(…))` pattern used
// throughout the app. The callback still fires once on mount and then every
// `intervalMs` milliseconds, but the interval is suspended whenever the browser
// tab is hidden (document.visibilityState === 'hidden'). When the tab becomes
// visible again the callback fires immediately and the interval resumes.
//
// This preserves the original polling semantics — callers see the same
// callback invocations when the tab is in focus, only background tabs stop
// sending traffic to the backend.

import { useEffect, useRef } from 'react';

export function useVisibleInterval(callback, intervalMs, deps = []) {
  // Keep the latest callback in a ref so we don't need to resubscribe
  // every render when a caller passes an inline arrow function.
  const savedCallback = useRef(callback);

  useEffect(() => {
    savedCallback.current = callback;
  }, [callback]);

  useEffect(() => {
    if (!intervalMs || intervalMs <= 0) return undefined;

    let intervalId = null;

    const tick = () => {
      try {
        savedCallback.current();
      } catch {
        // Swallow — caller is responsible for its own error handling.
      }
    };

    const start = () => {
      if (intervalId != null) return;
      tick();
      intervalId = setInterval(tick, intervalMs);
    };

    const stop = () => {
      if (intervalId == null) return;
      clearInterval(intervalId);
      intervalId = null;
    };

    const handleVisibility = () => {
      if (document.visibilityState === 'hidden') {
        stop();
      } else {
        start();
      }
    };

    if (document.visibilityState === 'visible') {
      start();
    }

    document.addEventListener('visibilitychange', handleVisibility);

    return () => {
      document.removeEventListener('visibilitychange', handleVisibility);
      stop();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs, ...deps]);
}
