// =============================================================================
// SetupContext.test.jsx — Pure-reducer tests for the wizard state machine
// =============================================================================
//
// The reducer is exported so we can drive every action without a render
// tree. Tests assert (a) the action mutates the right slice, (b) the
// returned state object is a new reference (immutability), and (c)
// SET_ROLE clears the orthogonal mode so the resulting profile is
// consistent with the handler's role/mode validation.

import { describe, it, expect } from 'vitest';
import {
  reducer,
  initialState,
  SETUP_ACTIONS,
} from '../../contexts/SetupContext.jsx';
import {
  MeshRole,
  MeshPointMode,
  MeshGateMode,
} from '../../gen/openmanet/setup/v1/setup_pb.js';
import { WifiEncryption } from '../../gen/openmanet/wifi_config/v1/wifi_config_pb.js';

describe('SetupContext.reducer', () => {
  it('SET_HOSTNAME mutates only hostname', () => {
    const next = reducer(initialState, { type: SETUP_ACTIONS.SET_HOSTNAME, value: 'mynode' });
    expect(next.hostname).toBe('mynode');
    expect(next).not.toBe(initialState);
    expect(next.mesh).toBe(initialState.mesh); // mesh slice untouched
  });

  it('SET_ROLE flipping to MESH_GATE clears meshpoint_mode and seeds gate mode', () => {
    const next = reducer(initialState, { type: SETUP_ACTIONS.SET_ROLE, value: MeshRole.MESH_GATE });
    expect(next.role).toBe(MeshRole.MESH_GATE);
    expect(next.meshpointMode).toBe(MeshPointMode.UNSPECIFIED);
    expect(next.meshgateMode).toBe(MeshGateMode.ROUTER);
  });

  it('SET_ROLE flipping back to MESH_POINT clears meshgate_mode and seeds point mode', () => {
    const gate = reducer(initialState, { type: SETUP_ACTIONS.SET_ROLE, value: MeshRole.MESH_GATE });
    const back = reducer(gate, { type: SETUP_ACTIONS.SET_ROLE, value: MeshRole.MESH_POINT });
    expect(back.role).toBe(MeshRole.MESH_POINT);
    expect(back.meshgateMode).toBe(MeshGateMode.UNSPECIFIED);
    expect(back.meshpointMode).toBe(MeshPointMode.EXTENDER);
  });

  it('SET_MESH_FIELD mutates only the named mesh slice', () => {
    const next = reducer(initialState, {
      type: SETUP_ACTIONS.SET_MESH_FIELD, field: 'channel', value: 36,
    });
    expect(next.mesh.channel).toBe(36);
    expect(next.mesh.meshId).toBe(initialState.mesh.meshId);
    expect(next.mesh).not.toBe(initialState.mesh);
  });

  it('SET_AP appends a new AP entry on first set', () => {
    const next = reducer(initialState, {
      type: SETUP_ACTIONS.SET_AP,
      value: { radioName: 'radio0', enabled: true, ssid: 'home', passphrase: 'longenough' },
    });
    expect(next.aps).toHaveLength(1);
    expect(next.aps[0].radioName).toBe('radio0');
    expect(next.aps[0].ssid).toBe('home');
    expect(next.aps[0].encryption).toBe(WifiEncryption.PSK2);
  });

  it('SET_AP merges into existing AP by radioName', () => {
    const seeded = reducer(initialState, {
      type: SETUP_ACTIONS.SET_AP,
      value: { radioName: 'radio0', enabled: true, ssid: 'home', passphrase: 'longenough' },
    });
    const merged = reducer(seeded, {
      type: SETUP_ACTIONS.SET_AP,
      value: { radioName: 'radio0', ssid: 'changed' },
    });
    expect(merged.aps).toHaveLength(1);
    expect(merged.aps[0].ssid).toBe('changed');
    expect(merged.aps[0].passphrase).toBe('longenough');
    expect(merged.aps[0].enabled).toBe(true);
  });

  it('REMOVE_AP filters by radioName', () => {
    const seeded = reducer(initialState, {
      type: SETUP_ACTIONS.SET_AP,
      value: { radioName: 'radio0', enabled: true },
    });
    const removed = reducer(seeded, { type: SETUP_ACTIONS.REMOVE_AP, radioName: 'radio0' });
    expect(removed.aps).toHaveLength(0);
  });

  it('RESET returns a fresh initial state', () => {
    const dirty = reducer(initialState, { type: SETUP_ACTIONS.SET_HOSTNAME, value: 'foo' });
    const reset = reducer(dirty, { type: SETUP_ACTIONS.RESET });
    expect(reset).toEqual(initialState);
  });

  it('HYDRATE_FROM_STATUS pre-fills mesh radio with first HaLow', () => {
    const next = reducer(initialState, {
      type: SETUP_ACTIONS.HYDRATE_FROM_STATUS,
      status: {
        radios: [
          { name: 'radio0', isHalow: false },
          { name: 'radio1', isHalow: true  },
        ],
      },
    });
    expect(next.mesh.radioName).toBe('radio1');
  });

  it('unknown action returns the same state reference', () => {
    const next = reducer(initialState, { type: 'NOT_A_REAL_ACTION' });
    expect(next).toBe(initialState);
  });
});
