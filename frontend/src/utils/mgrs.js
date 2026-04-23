// Lat/lon → MGRS grid string conversion (WGS-84).
//
// Pure math, no external deps. Matches the standard USNG/MGRS format:
//   "{zone}{band} {col}{row} {easting} {northing}"
// For example: "18S UJ 23456 07890" for Washington, DC.
//
// Valid input range: lat in [-80, 84), lon in [-180, 180). Out-of-range
// inputs return null (polar regions use UPS, not UTM/MGRS).

const DEG2RAD = Math.PI / 180;

// Latitude bands C–X, skipping I and O.
const LAT_BANDS = 'CDEFGHJKLMNPQRSTUVWX';

// 100 km column letter sets (one per 3-zone cycle).
const COL_LETTERS = ['ABCDEFGH', 'JKLMNPQR', 'STUVWXYZ'];

// 100 km row letter sequences, 20 letters skipping I and O.
// Odd zones start at 'A' at the equator; even zones start at 'F' (offset 5).
const ROW_LETTERS_ODD = 'ABCDEFGHJKLMNPQRSTUV';
const ROW_LETTERS_EVEN = 'FGHJKLMNPQRSTUVABCDE';

function latBand(lat) {
  if (lat < -80 || lat >= 84) return null;
  // Band X (72–84) is 12° wide, not 8°. Everything else is 8°.
  if (lat >= 72) return 'X';
  return LAT_BANDS[Math.floor((lat + 80) / 8)];
}

// UTM zone number, with Norway/Svalbard corrections.
function utmZone(lat, lon) {
  let zone = Math.floor((lon + 180) / 6) + 1;
  // Norway special case: zone 32 extends west to cover parts of 31.
  if (lat >= 56 && lat < 64 && lon >= 3 && lon < 12) zone = 32;
  // Svalbard special cases (band X).
  if (lat >= 72 && lat < 84) {
    if (lon >= 0 && lon < 9) zone = 31;
    else if (lon >= 9 && lon < 21) zone = 33;
    else if (lon >= 21 && lon < 33) zone = 35;
    else if (lon >= 33 && lon < 42) zone = 37;
  }
  return zone;
}

function latLonToUTM(lat, lon) {
  const a = 6378137.0;
  const f = 1 / 298.257223563;
  const k0 = 0.9996;
  const e2 = 2 * f - f * f;
  const ep2 = e2 / (1 - e2);

  const zone = utmZone(lat, lon);
  const lambda0 = ((zone - 1) * 6 - 180 + 3) * DEG2RAD;

  const phi = lat * DEG2RAD;
  const lambda = lon * DEG2RAD;

  const sinPhi = Math.sin(phi);
  const cosPhi = Math.cos(phi);
  const tanPhi = Math.tan(phi);

  const N = a / Math.sqrt(1 - e2 * sinPhi * sinPhi);
  const T = tanPhi * tanPhi;
  const C = ep2 * cosPhi * cosPhi;
  const A = cosPhi * (lambda - lambda0);

  const M = a * (
    (1 - e2 / 4 - 3 * e2 * e2 / 64 - 5 * e2 * e2 * e2 / 256) * phi
    - (3 * e2 / 8 + 3 * e2 * e2 / 32 + 45 * e2 * e2 * e2 / 1024) * Math.sin(2 * phi)
    + (15 * e2 * e2 / 256 + 45 * e2 * e2 * e2 / 1024) * Math.sin(4 * phi)
    - (35 * e2 * e2 * e2 / 3072) * Math.sin(6 * phi)
  );

  const A2 = A * A;
  const A3 = A2 * A;
  const A4 = A3 * A;
  const A5 = A4 * A;
  const A6 = A5 * A;

  const easting = k0 * N * (
    A + (1 - T + C) * A3 / 6
    + (5 - 18 * T + T * T + 72 * C - 58 * ep2) * A5 / 120
  ) + 500000;

  let northing = k0 * (M + N * tanPhi * (
    A2 / 2 + (5 - T + 9 * C + 4 * C * C) * A4 / 24
    + (61 - 58 * T + T * T + 600 * C - 330 * ep2) * A6 / 720
  ));

  if (lat < 0) northing += 10000000;

  return { zone, easting, northing };
}

function utmToMGRS(zone, easting, northing, lat, precision) {
  const band = latBand(lat);
  if (!band) return null;

  const colSet = COL_LETTERS[(zone - 1) % 3];
  const colIdx = Math.floor(easting / 100000) - 1;
  if (colIdx < 0 || colIdx >= colSet.length) return null;
  const colLetter = colSet[colIdx];

  const rowSet = zone % 2 === 1 ? ROW_LETTERS_ODD : ROW_LETTERS_EVEN;
  const rowIdx = Math.floor(northing / 100000) % 20;
  const rowLetter = rowSet[rowIdx];

  const eastingMod = Math.floor(easting % 100000);
  const northingMod = Math.floor(northing % 100000);

  const ePart = eastingMod.toString().padStart(5, '0').slice(0, precision);
  const nPart = northingMod.toString().padStart(5, '0').slice(0, precision);

  return `${zone}${band} ${colLetter}${rowLetter} ${ePart} ${nPart}`;
}

export function latLonToMGRS(lat, lon, precision = 5) {
  if (lat == null || lon == null) return null;
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
  if (lat < -80 || lat >= 84) return null;
  if (lon < -180 || lon >= 180) return null;
  if (precision < 1 || precision > 5) return null;

  const { zone, easting, northing } = latLonToUTM(lat, lon);
  return utmToMGRS(zone, easting, northing, lat, precision);
}
