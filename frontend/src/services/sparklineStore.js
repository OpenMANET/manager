// =============================================================================
// sparklineStore — module-scoped rolling sample history
// =============================================================================
//
// Retains sparkline sample series across React mount/unmount cycles so
// that navigating away from a page and back preserves the visible
// history (Dashboard link-quality, BLOS rx/tx, etc.) instead of
// restarting at an empty array. Series are keyed by an opaque string;
// callers pick the key and a capacity. Samples outside the capacity
// are trimmed from the head.
//
// The store is a plain in-memory singleton bound to the module's
// lifetime. It does NOT persist across full-page reloads (that would
// require localStorage and a schema-versioning story for a cosmetic
// feature — overkill). Within a session, however, every sample a
// subscriber pushes is durable until the tab closes.

import { useSyncExternalStore } from 'react';

const series = new Map(); // key → { samples: [], listeners: Set<() => void> }

function ensure(key) {
  let s = series.get(key);
  if (!s) {
    s = { samples: [], listeners: new Set() };
    series.set(key, s);
  }
  return s;
}

function notify(s) {
  // Defensive copy: a listener mutating the set during iteration
  // would otherwise skip entries or re-enter.
  for (const cb of Array.from(s.listeners)) {
    try {
      cb();
    } catch {
      // Subscriber errors mustn't prevent the remaining listeners
      // from firing — swallow and keep going.
    }
  }
}

// pushSparklineSample appends `value` to the named series, trimming
// the head so the array never exceeds `cap` entries. Returns nothing;
// subscribers are notified so React re-renders pick up the change.
// Non-finite inputs are coerced to 0 so canvas drawing doesn't NaN-out.
export function pushSparklineSample(key, value, cap) {
  const s = ensure(key);
  const numeric = Number.isFinite(value) ? value : 0;
  const next = s.samples.length >= cap
    ? [...s.samples.slice(s.samples.length - cap + 1), numeric]
    : [...s.samples, numeric];
  s.samples = next;
  notify(s);
}

// getSparklineSamples returns the current array for a series. Safe to
// call outside of React.
export function getSparklineSamples(key) {
  return ensure(key).samples;
}

// useSparklineSamples subscribes a React component to a series. The
// returned array reference changes whenever pushSparklineSample mutates
// it, so standard React referential-equality skips work as expected.
export function useSparklineSamples(key) {
  return useSyncExternalStore(
    (cb) => {
      const s = ensure(key);
      s.listeners.add(cb);
      return () => s.listeners.delete(cb);
    },
    () => ensure(key).samples,
    // SSR snapshot — never rendered here but required by the hook.
    () => ensure(key).samples,
  );
}

// resetSparklineSeries clears a series. Test-only; callers in app code
// should never invoke this so the "history persists across mounts"
// contract stays honest.
export function resetSparklineSeries(key) {
  const s = series.get(key);
  if (!s) return;
  s.samples = [];
  notify(s);
}

// resetAllSparklineSeries clears every series. Test hook — same caveat
// as resetSparklineSeries.
export function resetAllSparklineSeries() {
  for (const key of series.keys()) resetSparklineSeries(key);
}
