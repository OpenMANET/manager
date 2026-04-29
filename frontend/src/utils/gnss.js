// GNSS display helpers — derivations of fields the proto doesn't carry.
//
// All functions are pure. Import individually; no shared state.

// PRN ranges per the standard GNSS numbering conventions used by u-blox
// and most NMEA-speaking receivers. Unknown ranges default to "GPS" so
// the table never shows a blank cell for an obscure SV.
export function prnToConstellation(prn) {
  if (prn == null || !Number.isFinite(prn)) return 'GPS';
  if (prn >= 1 && prn <= 32) return 'GPS';
  if (prn >= 33 && prn <= 64) return 'SBAS';
  if (prn >= 65 && prn <= 96) return 'GLONASS';
  if (prn >= 193 && prn <= 200) return 'QZSS';
  if (prn >= 201 && prn <= 237) return 'BEIDOU';
  if (prn >= 301 && prn <= 336) return 'GALILEO';
  return 'GPS';
}

// Maps the proto Constellation enum (numeric) to the same upper-case label
// vocabulary as prnToConstellation. UNSPECIFIED returns null so callers can
// fall back to PRN-range derivation.
const CONSTELLATION_BY_ENUM = {
  1: 'GPS',
  2: 'SBAS',
  3: 'GALILEO',
  4: 'BEIDOU',
  5: 'IMES',
  6: 'QZSS',
  7: 'GLONASS',
  8: 'IRNSS',
};

// satelliteConstellation prefers the proto-supplied constellation field; if
// the receiver/daemon couldn't identify it (UNSPECIFIED), falls back to a
// PRN-range derivation so older daemons still produce a usable label.
export function satelliteConstellation(sat) {
  if (!sat) return 'GPS';
  const fromProto = CONSTELLATION_BY_ENUM[sat.constellation];
  if (fromProto) return fromProto;
  return prnToConstellation(sat.prn);
}

// CEP 95% estimate in meters from HDOP. Assumes σ_UERE ≈ 3 m (typical
// consumer receiver) and scales to the 95% confidence ring:
//   CEP95 ≈ HDOP × σ_UERE × 1.52
// The caller should label the value as estimated.
export function estimateCEP95(hdop) {
  if (hdop == null || !Number.isFinite(hdop) || hdop <= 0) return null;
  return hdop * 4.6;
}

// Rolling-window fix rate in Hz. `timestamps` is an array of Date /
// epoch-ms values in any order; at least 2 distinct samples are needed.
// Returns one of the common rates (1, 2, 5, 10 Hz) or null.
export function computeFixRateHz(timestamps) {
  if (!Array.isArray(timestamps) || timestamps.length < 2) return null;

  const ms = timestamps
    .map((t) => (t instanceof Date ? t.getTime() : Number(t)))
    .filter((n) => Number.isFinite(n));
  if (ms.length < 2) return null;

  ms.sort((a, b) => a - b);
  const deltas = [];
  for (let i = 1; i < ms.length; i++) {
    const d = ms[i] - ms[i - 1];
    if (d > 0) deltas.push(d);
  }
  if (deltas.length === 0) return null;

  deltas.sort((a, b) => a - b);
  const medianMs = deltas[Math.floor(deltas.length / 2)];
  if (medianMs <= 0) return null;

  const hz = 1000 / medianMs;
  if (hz >= 7.5) return 10;
  if (hz >= 3.5) return 5;
  if (hz >= 1.5) return 2;
  return 1;
}

// Compact "X ago" formatter for GNSS freshness indicators. Accepts a Date
// or ISO-string / proto Timestamp value; returns null when the input is
// missing or unparseable so callers can render a dash placeholder.
export function formatAgo(ts, now = Date.now()) {
  if (ts == null) return null;
  const d = ts instanceof Date ? ts : new Date(ts);
  const t = d.getTime();
  if (Number.isNaN(t)) return null;
  const deltaMs = now - t;
  if (deltaMs < 1500) return 'just now';
  const s = Math.floor(deltaMs / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

// HDOP/PDOP chip severity bucket matching the mockup color bands.
// Thresholds follow common GNSS practice: ≤ 2 is "ideal/excellent",
// 2–5 is "good/moderate", > 5 is "fair/poor".
export function dopSeverity(dop) {
  if (dop == null || !Number.isFinite(dop) || dop <= 0) return '';
  if (dop <= 2) return 'ok';
  if (dop <= 5) return 'warn';
  return 'crit';
}
