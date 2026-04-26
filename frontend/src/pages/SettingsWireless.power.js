// =============================================================================
// SettingsWireless.power.js — power-level mapping for the Wireless settings UI
// =============================================================================
//
// Operators select TX power as Low / Medium / High / Max in the UI; the wire
// format remains an int32 dBm value validated [0, 30] by the backend.

export const POWER_LEVELS = [
  { level: 'low',    label: 'Low',    dBm: 10 },
  { level: 'medium', label: 'Medium', dBm: 17 },
  { level: 'high',   label: 'High',   dBm: 23 },
  { level: 'max',    label: 'Max',    dBm: 30 },
];

// dBmToLevel snaps a raw dBm reading to the nearest canonical level. Ties
// favour the higher level so a pre-provisioned non-canonical value rounds
// toward "more capability".
export function dBmToLevel(dBm) {
  if (dBm == null || Number.isNaN(dBm)) return 'medium';
  let best = POWER_LEVELS[0];
  let bestDist = Math.abs(dBm - best.dBm);
  for (const p of POWER_LEVELS) {
    const d = Math.abs(dBm - p.dBm);
    if (d <= bestDist) { best = p; bestDist = d; }
  }
  return best.level;
}

export function levelToDbm(level) {
  return POWER_LEVELS.find(p => p.level === level)?.dBm ?? 17;
}
