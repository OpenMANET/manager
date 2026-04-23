// =============================================================================
// mgrs.test.js — Tests for lat/lon → MGRS conversion
// =============================================================================

import { describe, it, expect } from 'vitest';
import { latLonToMGRS } from '../../utils/mgrs.js';

describe('TestLatLonToMGRS', () => {
  // Fixture points verified against NGA MGRS calculator. 5-digit precision
  // is accurate to 1 meter; the fixture allows minor last-digit rounding.
  const fixtures = [
    // Washington, DC (White House area)
    { lat: 38.8976, lon: -77.0365, zone: '18', band: 'S', square: 'UJ' },
    // Null Island (0, 0)
    { lat: 0, lon: 0, zone: '31', band: 'N', square: 'AA' },
    // London (Big Ben)
    { lat: 51.5007, lon: -0.1246, zone: '30', band: 'U', square: 'XC' },
    // Sydney Opera House
    { lat: -33.8568, lon: 151.2153, zone: '56', band: 'H', square: 'LH' },
    // Cape Town
    { lat: -33.9249, lon: 18.4241, zone: '34', band: 'H', square: 'BH' },
  ];

  fixtures.forEach(({ lat, lon, zone, band, square }) => {
    it(`converts ${lat}, ${lon} to a valid MGRS string`, () => {
      const result = latLonToMGRS(lat, lon);
      expect(result).not.toBeNull();
      expect(result).toMatch(/^\d{1,2}[A-Z] [A-Z]{2} \d{5} \d{5}$/);
      const parts = result.split(' ');
      expect(parts[0]).toBe(zone + band);
      expect(parts[1]).toBe(square);
    });
  });

  it('honors precision parameter', () => {
    const p5 = latLonToMGRS(38.8976, -77.0365, 5);
    const p3 = latLonToMGRS(38.8976, -77.0365, 3);
    const p1 = latLonToMGRS(38.8976, -77.0365, 1);

    expect(p5.split(' ').slice(2).join('')).toHaveLength(10);
    expect(p3.split(' ').slice(2).join('')).toHaveLength(6);
    expect(p1.split(' ').slice(2).join('')).toHaveLength(2);
  });

  it('returns null for polar latitudes', () => {
    expect(latLonToMGRS(85, 0)).toBeNull();
    expect(latLonToMGRS(-85, 0)).toBeNull();
    expect(latLonToMGRS(84, 0)).toBeNull();
  });

  it('returns null for invalid inputs', () => {
    expect(latLonToMGRS(null, 0)).toBeNull();
    expect(latLonToMGRS(0, null)).toBeNull();
    expect(latLonToMGRS(NaN, 0)).toBeNull();
    expect(latLonToMGRS(0, NaN)).toBeNull();
    expect(latLonToMGRS(0, 200)).toBeNull();
    expect(latLonToMGRS(0, -200)).toBeNull();
  });

  it('rejects precision outside 1-5', () => {
    expect(latLonToMGRS(0, 0, 0)).toBeNull();
    expect(latLonToMGRS(0, 0, 6)).toBeNull();
  });

  it('preserves zone letter boundaries', () => {
    // Band N starts at equator (0°); band M is just south of it.
    expect(latLonToMGRS(0.1, 0)).toMatch(/^31N /);
    expect(latLonToMGRS(-0.1, 0)).toMatch(/^31M /);
  });
});
