// =============================================================================
// pollStore — reference-counted shared polling store
// =============================================================================
//
// Creates a module-scoped store with a single background poller shared
// across all React subscribers. The poller only runs while at least one
// subscriber is mounted, stops when the tab is hidden, and ticks at the
// minimum interval requested by any current subscriber.
//
// This lets multiple pages that render the same data (Dashboard + Topology,
// Dashboard + GpsStatus, etc.) share a single in-flight RPC per cycle
// instead of each firing its own independent setInterval.
//
// Preserves existing semantics: the data a subscriber receives is the
// same data the page would have fetched on its own; subscribers simply
// no longer trigger duplicate RPCs.

// createPollStore returns { subscribe, getSnapshot }. `fetcher` is an
// async function invoked on each tick; its resolved value is published
// to all current subscribers and exposed via getSnapshot.
export function createPollStore(fetcher) {
  let snapshot = null;
  const subscribers = new Set();
  let timerId = null;
  let inFlight = false;
  let visible =
    typeof document === 'undefined' || document.visibilityState !== 'hidden';
  let visibilityBound = false;

  function notify() {
    for (const sub of subscribers) {
      try {
        sub.onChange(snapshot);
      } catch {
        // Subscribers own their own error handling.
      }
    }
  }

  async function tick() {
    if (inFlight) return;
    inFlight = true;
    try {
      const next = await fetcher();
      snapshot = next;
      notify();
    } catch {
      // Non-fatal: keep previous snapshot. Subscribers get stale data
      // rather than crashing, same as the original per-page try/catch.
    } finally {
      inFlight = false;
    }
  }

  function minInterval() {
    let min = Infinity;
    for (const sub of subscribers) {
      if (sub.intervalMs < min) min = sub.intervalMs;
    }
    return min === Infinity ? 0 : min;
  }

  function restartTimer() {
    if (timerId != null) {
      clearInterval(timerId);
      timerId = null;
    }
    if (!visible || subscribers.size === 0) return;
    const ms = minInterval();
    if (ms > 0) timerId = setInterval(tick, ms);
  }

  function onVisibilityChange() {
    const nowVisible = document.visibilityState !== 'hidden';
    if (nowVisible === visible) return;
    visible = nowVisible;
    restartTimer();
    if (visible && subscribers.size > 0) tick();
  }

  function bindVisibility() {
    if (visibilityBound || typeof document === 'undefined') return;
    document.addEventListener('visibilitychange', onVisibilityChange);
    visibilityBound = true;
  }

  function unbindVisibility() {
    if (!visibilityBound || typeof document === 'undefined') return;
    document.removeEventListener('visibilitychange', onVisibilityChange);
    visibilityBound = false;
  }

  function subscribe(intervalMs, onChange) {
    const sub = { intervalMs, onChange };
    subscribers.add(sub);
    bindVisibility();
    restartTimer();

    // Hand the late subscriber the cached snapshot immediately so the UI
    // shows the last known data while the next tick fetches a fresh one,
    // and always kick off a tick so a freshly mounted page never has to
    // wait a full interval (or sit on stale data) to see live values.
    if (snapshot !== null) {
      try {
        onChange(snapshot);
      } catch {
        /* ignore */
      }
    }
    if (visible) tick();

    return () => {
      subscribers.delete(sub);
      if (subscribers.size === 0) {
        if (timerId != null) {
          clearInterval(timerId);
          timerId = null;
        }
        unbindVisibility();
        // Keep the snapshot — the next mount renders cached data instantly
        // while subscribe() kicks off a fresh tick.
        inFlight = false;
      } else {
        restartTimer();
      }
    };
  }

  function getSnapshot() {
    return snapshot;
  }

  return { subscribe, getSnapshot };
}
