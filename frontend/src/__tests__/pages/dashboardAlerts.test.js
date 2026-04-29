// =============================================================================
// dashboardAlerts.test.js — pure helpers driving the Dashboard alerts panel
// =============================================================================

import { describe, it, expect } from 'vitest';
import { classifyAlerts, findLostPeers } from '../../pages/dashboardAlerts.js';

describe('TestFindLostPeers', () => {
  const LOST_MS = 30_000;
  const FORGET_MS = 300_000;

  it('returns peers that fell out of the present set after lostMs', () => {
    const now = 1_000_000;
    const history = {
      ALPHA: now - 60_000, // 60s ago — past the lost threshold
      BRAVO: now - 5_000,  // recent — but still present so should not alert
    };
    const lost = findLostPeers(history, new Set(['BRAVO']), now, LOST_MS, FORGET_MS);
    expect(lost).toHaveLength(1);
    expect(lost[0].name).toBe('ALPHA');
  });

  it('does not alert for peers within the freshness window', () => {
    const now = 1_000_000;
    const history = { ALPHA: now - 10_000 };
    const lost = findLostPeers(history, new Set(), now, LOST_MS, FORGET_MS);
    expect(lost).toEqual([]);
  });

  it('forgets peers that have been gone longer than forgetMs', () => {
    const now = 1_000_000;
    const history = { ALPHA: now - 600_000 }; // 10 minutes — past forget window
    const lost = findLostPeers(history, new Set(), now, LOST_MS, FORGET_MS);
    expect(lost).toEqual([]);
  });

  it('skips peers that are still in the current peer list', () => {
    const now = 1_000_000;
    const history = { ALPHA: now - 10 * 60_000, BRAVO: now - 60_000 };
    const lost = findLostPeers(history, new Set(['ALPHA']), now, LOST_MS, FORGET_MS);
    expect(lost.map((p) => p.name)).toEqual(['BRAVO']);
  });

  it('sorts the most-recently lost peer first', () => {
    const now = 1_000_000;
    const history = {
      OLDER: now - 120_000,
      NEWER: now - 45_000,
    };
    const lost = findLostPeers(history, new Set(), now, LOST_MS, FORGET_MS);
    expect(lost.map((p) => p.name)).toEqual(['NEWER', 'OLDER']);
  });

  it('accepts an iterable as the present argument', () => {
    const now = 1_000_000;
    const history = { ALPHA: now - 60_000, BRAVO: now - 60_000 };
    const lost = findLostPeers(history, ['ALPHA'], now, LOST_MS, FORGET_MS);
    expect(lost.map((p) => p.name)).toEqual(['BRAVO']);
  });
});

describe('TestClassifyAlerts', () => {
  it('emits MESH UP when connected and no other signal is present', () => {
    const alerts = classifyAlerts({ mesh: { status: { connected: true } } });
    expect(alerts).toEqual([
      { level: 'ok', text: 'MESH UP · CONVERGED' },
    ]);
  });

  it('emits MESH DOWN when not connected', () => {
    const alerts = classifyAlerts({ mesh: { status: { connected: false } } });
    expect(alerts[0]).toEqual({ level: 'crit', text: 'MESH DOWN · NO NEIGHBORS' });
  });

  it('emits a warn alert for each lost peer with the hostname', () => {
    const alerts = classifyAlerts({
      mesh: { status: { connected: true } },
      lostPeers: [{ name: 'BRAVO', ageMs: 45_000 }, { name: 'CHARLIE', ageMs: 90_000 }],
    });
    expect(alerts).toContainEqual({ level: 'warn', text: 'PEER BRAVO · DISCONNECTED' });
    expect(alerts).toContainEqual({ level: 'warn', text: 'PEER CHARLIE · DISCONNECTED' });
  });

  it('does not emit any TQ-derived alert (BATMAN_V deployment)', () => {
    const alerts = classifyAlerts({
      mesh: { status: { connected: true } },
      lostPeers: [],
    });
    for (const a of alerts) {
      expect(a.text).not.toMatch(/TQ/);
      expect(a.text).not.toMatch(/DEGRADED/);
    }
  });

  it('emits ROUTES LOST when delta.routesLost > 0', () => {
    const alerts = classifyAlerts({
      mesh: { status: { connected: true } },
      delta: { routesLost: 2, routesAdded: 0 },
    });
    expect(alerts).toContainEqual({ level: 'warn', text: '2 ROUTES LOST · 60s' });
  });

  it('emits MESH HEALED only when routes were added without losses', () => {
    const healed = classifyAlerts({
      mesh: { status: { connected: true } },
      delta: { routesLost: 0, routesAdded: 3 },
    });
    expect(healed).toContainEqual({ level: 'ok', text: 'MESH HEALED · +3 ROUTES' });

    const churn = classifyAlerts({
      mesh: { status: { connected: true } },
      delta: { routesLost: 1, routesAdded: 3 },
    });
    expect(churn.some((a) => a.text.startsWith('MESH HEALED'))).toBe(false);
  });

  it('caps the output at six entries', () => {
    const alerts = classifyAlerts({
      mesh: { status: { connected: true } },
      lostPeers: Array.from({ length: 12 }, (_, i) => ({ name: `H${i}`, ageMs: 60_000 })),
    });
    expect(alerts.length).toBe(6);
  });
});
