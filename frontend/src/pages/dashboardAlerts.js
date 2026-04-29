// =============================================================================
// dashboardAlerts.js — pure helpers for the Dashboard alerts panel
// =============================================================================
//
// Extracted out of Dashboard.jsx so the page file can stay JSX-only (keeps
// React Fast Refresh happy) and so these can be unit-tested without
// rendering the full page.

// findLostPeers returns peers we have seen recently but that are missing
// from the current peer list. peerHistory is a plain object keyed by
// upper-cased hostname mapping to the lastSeenMs timestamp. A peer enters
// the "lost" window once it has been missing for at least lostMs and
// leaves it after forgetMs, so the alert auto-clears for peers that stay
// gone.
export function findLostPeers(peerHistory, currentNamesUpper, nowMs, lostMs, forgetMs) {
  const present = currentNamesUpper instanceof Set
    ? currentNamesUpper
    : new Set(currentNamesUpper);
  const out = [];
  for (const [name, lastSeenMs] of Object.entries(peerHistory)) {
    if (!name || present.has(name)) continue;
    const ageMs = nowMs - lastSeenMs;
    if (ageMs >= lostMs && ageMs < forgetMs) {
      out.push({ name, ageMs });
    }
  }
  out.sort((a, b) => a.ageMs - b.ageMs);
  return out;
}

export function classifyAlerts({ mesh, lostPeers = [], delta }) {
  const out = [];
  if (mesh?.status?.connected) {
    out.push({ level: 'ok', text: 'MESH UP · CONVERGED' });
  } else {
    out.push({ level: 'crit', text: 'MESH DOWN · NO NEIGHBORS' });
  }
  for (const p of lostPeers) {
    out.push({ level: 'warn', text: `PEER ${p.name} · DISCONNECTED` });
  }
  if (delta?.routesLost > 0) {
    out.push({ level: 'warn', text: `${delta.routesLost} ROUTE${delta.routesLost > 1 ? 'S' : ''} LOST · 60s` });
  }
  if (delta?.routesAdded > 0 && (delta?.routesLost ?? 0) === 0) {
    out.push({ level: 'ok', text: `MESH HEALED · +${delta.routesAdded} ROUTES` });
  }
  return out.slice(0, 6);
}
