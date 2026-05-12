// =============================================================================
// gnss.test.js — Tests for GNSS display helpers
// =============================================================================

import { describe, it, expect } from 'vitest';
import {
  prnToConstellation,
  estimateCEP95,
  computeFixRateHz,
  dopSeverity,
} from '../../utils/gnss.js';

describe('TestPrnToConstellation', () => {
  it('maps GPS PRN range', () => {
    expect(prnToConstellation(1)).toBe('GPS');
    expect(prnToConstellation(15)).toBe('GPS');
    expect(prnToConstellation(32)).toBe('GPS');
  });

  it('maps SBAS PRN range', () => {
    expect(prnToConstellation(33)).toBe('SBAS');
    expect(prnToConstellation(50)).toBe('SBAS');
    expect(prnToConstellation(64)).toBe('SBAS');
  });

  it('maps GLONASS PRN range', () => {
    expect(prnToConstellation(65)).toBe('GLONASS');
    expect(prnToConstellation(80)).toBe('GLONASS');
    expect(prnToConstellation(96)).toBe('GLONASS');
  });

  it('maps QZSS PRN range', () => {
    expect(prnToConstellation(193)).toBe('QZSS');
    expect(prnToConstellation(200)).toBe('QZSS');
  });

  it('maps BeiDou PRN range', () => {
    expect(prnToConstellation(201)).toBe('BEIDOU');
    expect(prnToConstellation(237)).toBe('BEIDOU');
  });

  it('maps Galileo PRN range', () => {
    expect(prnToConstellation(301)).toBe('GALILEO');
    expect(prnToConstellation(336)).toBe('GALILEO');
  });

  it('defaults unknown PRNs to GPS', () => {
    expect(prnToConstellation(150)).toBe('GPS');
    expect(prnToConstellation(500)).toBe('GPS');
  });

  it('handles null and non-finite inputs', () => {
    expect(prnToConstellation(null)).toBe('GPS');
    expect(prnToConstellation(undefined)).toBe('GPS');
    expect(prnToConstellation(NaN)).toBe('GPS');
  });
});

describe('TestEstimateCEP95', () => {
  it('scales HDOP by 4.6', () => {
    expect(estimateCEP95(1)).toBeCloseTo(4.6);
    expect(estimateCEP95(0.5)).toBeCloseTo(2.3);
    expect(estimateCEP95(2)).toBeCloseTo(9.2);
  });

  it('returns null for missing or zero HDOP', () => {
    expect(estimateCEP95(null)).toBeNull();
    expect(estimateCEP95(undefined)).toBeNull();
    expect(estimateCEP95(0)).toBeNull();
    expect(estimateCEP95(-1)).toBeNull();
    expect(estimateCEP95(NaN)).toBeNull();
  });
});

describe('TestComputeFixRateHz', () => {
  it('returns null for too few samples', () => {
    expect(computeFixRateHz(null)).toBeNull();
    expect(computeFixRateHz([])).toBeNull();
    expect(computeFixRateHz([Date.now()])).toBeNull();
  });

  it('computes 1 Hz from 1-second spacing', () => {
    const base = 1_700_000_000_000;
    const ts = [base, base + 1000, base + 2000, base + 3000];
    expect(computeFixRateHz(ts)).toBe(1);
  });

  it('computes 5 Hz from 200 ms spacing', () => {
    const base = 1_700_000_000_000;
    const ts = [base, base + 200, base + 400, base + 600];
    expect(computeFixRateHz(ts)).toBe(5);
  });

  it('computes 10 Hz from 100 ms spacing', () => {
    const base = 1_700_000_000_000;
    const ts = [base, base + 100, base + 200, base + 300];
    expect(computeFixRateHz(ts)).toBe(10);
  });

  it('accepts Date objects', () => {
    const ts = [new Date(1000), new Date(2000), new Date(3000)];
    expect(computeFixRateHz(ts)).toBe(1);
  });

  it('buckets 2 Hz from 500 ms spacing', () => {
    const base = 1_700_000_000_000;
    const ts = [base, base + 500, base + 1000];
    expect(computeFixRateHz(ts)).toBe(2);
  });
});

describe('TestDopSeverity', () => {
  it('classifies excellent DOP as ok', () => {
    expect(dopSeverity(0.8)).toBe('ok');
    expect(dopSeverity(1.5)).toBe('ok');
    expect(dopSeverity(2)).toBe('ok');
  });

  it('classifies moderate DOP as warn', () => {
    expect(dopSeverity(2.1)).toBe('warn');
    expect(dopSeverity(3.5)).toBe('warn');
    expect(dopSeverity(5)).toBe('warn');
  });

  it('classifies poor DOP as crit', () => {
    expect(dopSeverity(5.1)).toBe('crit');
    expect(dopSeverity(10)).toBe('crit');
  });

  it('returns empty string for invalid input', () => {
    expect(dopSeverity(null)).toBe('');
    expect(dopSeverity(0)).toBe('');
    expect(dopSeverity(-1)).toBe('');
    expect(dopSeverity(NaN)).toBe('');
  });
});
