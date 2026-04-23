// =============================================================================
// pollStore.test.js — tests for the reference-counted shared polling store
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createPollStore } from '../services/pollStore.js';

function setVisibility(state) {
  Object.defineProperty(document, 'visibilityState', {
    value: state,
    configurable: true,
    writable: true,
  });
  Object.defineProperty(document, 'hidden', {
    value: state === 'hidden',
    configurable: true,
    writable: true,
  });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('TestPollStore', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setVisibility('visible');
  });

  afterEach(() => {
    vi.useRealTimers();
    setVisibility('visible');
  });

  it('primes a new subscriber with an immediate fetch', async () => {
    const fetcher = vi.fn().mockResolvedValue({ n: 1 });
    const store = createPollStore(fetcher);
    const onChange = vi.fn();

    store.subscribe(1000, onChange);
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    await vi.waitFor(() => expect(onChange).toHaveBeenCalledWith({ n: 1 }));
  });

  it('shares one fetch across multiple subscribers', async () => {
    const fetcher = vi.fn().mockResolvedValue({ n: 2 });
    const store = createPollStore(fetcher);

    const a = vi.fn();
    const b = vi.fn();
    store.subscribe(1000, a);
    await vi.waitFor(() => expect(a).toHaveBeenCalledWith({ n: 2 }));

    store.subscribe(1000, b);
    // Late subscriber gets the cached snapshot synchronously.
    expect(b).toHaveBeenCalledWith({ n: 2 });
    // No additional fetch was triggered by subscribe.
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('ticks at the minimum of subscribers intervals', async () => {
    const fetcher = vi.fn().mockResolvedValue({ n: 3 });
    const store = createPollStore(fetcher);

    store.subscribe(10_000, vi.fn());
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));

    store.subscribe(1000, vi.fn());

    vi.advanceTimersByTime(1000);
    // The faster subscriber's cadence should drive the interval.
    await vi.waitFor(() => expect(fetcher.mock.calls.length).toBeGreaterThanOrEqual(2));
  });

  it('stops polling when the tab becomes hidden and resumes on visible', async () => {
    const fetcher = vi.fn().mockResolvedValue({ n: 4 });
    const store = createPollStore(fetcher);

    store.subscribe(1000, vi.fn());
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));

    setVisibility('hidden');
    vi.advanceTimersByTime(5000);
    expect(fetcher).toHaveBeenCalledTimes(1);

    setVisibility('visible');
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  });

  it('drops cached snapshot when the last subscriber unsubscribes', async () => {
    const fetcher = vi.fn().mockResolvedValue({ n: 5 });
    const store = createPollStore(fetcher);
    const onChange = vi.fn();

    const unsub = store.subscribe(1000, onChange);
    await vi.waitFor(() => expect(store.getSnapshot()).toEqual({ n: 5 }));

    unsub();
    expect(store.getSnapshot()).toBeNull();
  });

  it('does not double-run when multiple subscribe calls fire during an in-flight fetch', async () => {
    let resolveFetch;
    const fetcher = vi.fn(
      () => new Promise((r) => { resolveFetch = r; }),
    );
    const store = createPollStore(fetcher);

    store.subscribe(1000, vi.fn());
    store.subscribe(500, vi.fn());

    // Both subscribes happened; only one fetch is in flight.
    expect(fetcher).toHaveBeenCalledTimes(1);

    resolveFetch({ n: 6 });
    await vi.waitFor(() => expect(store.getSnapshot()).toEqual({ n: 6 }));
  });
});
