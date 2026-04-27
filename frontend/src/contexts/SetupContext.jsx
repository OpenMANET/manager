// =============================================================================
// SetupContext.jsx — Wizard state machine over MeshNodeProfile
// =============================================================================
//
// The wizard lives entirely in client memory: a browser refresh wipes the
// in-progress profile and restarts at Step 1 (matching the plan's "no
// server-side draft" decision). State is shaped to mirror MeshNodeProfile
// so the Review step can submit it directly without a copy/transform pass.
//
// Each action only mutates one slice; the full state object is replaced on
// every dispatch so React.memo'd children re-render only when their slice
// changed (the reducer never returns the same reference unless no field
// actually changed). Tests in __tests__/contexts/SetupContext.test.jsx
// exercise every action plus immutability.

import { createContext, useContext, useReducer, useMemo } from 'react';
import {
  MeshRole,
  MeshPointMode,
  MeshGateMode,
  UplinkType,
} from '../gen/openmanet/setup/v1/setup_pb.js';
import { WifiEncryption } from '../gen/openmanet/wifi_config/v1/wifi_config_pb.js';

// initialState mirrors the MeshNodeProfile shape with Mesh Point selected
// by default (plan: "Mesh Point should be selected by default") and the
// canonical S1G defaults (2 MHz / channel 42).
export const initialState = {
  hostname:       '',
  adminPassword:  '',
  role:           MeshRole.MESH_POINT,
  meshpointMode:  MeshPointMode.EXTENDER,
  meshgateMode:   MeshGateMode.UNSPECIFIED,
  mesh: {
    radioName:    '',
    meshId:       'openmanet',
    passphrase:   '',
    encryption:   WifiEncryption.SAE,
    bandwidthMhz: 2,
    channel:      42,
  },
  uplink: {
    type:           UplinkType.UNSPECIFIED,
    ethernetPort:   '',
    wireless: {
      radioName:  '',
      ssid:       '',
      passphrase: '',
      encryption: WifiEncryption.PSK2,
    },
  },
  aps: [],
  // adminPasswordConfirm is UI-only — never sent to the backend; just
  // forces the user to type the same password twice.
  adminPasswordConfirm: '',
};

export const SETUP_ACTIONS = {
  SET_HOSTNAME:           'SET_HOSTNAME',
  SET_ROLE:               'SET_ROLE',
  SET_MESHPOINT_MODE:     'SET_MESHPOINT_MODE',
  SET_MESHGATE_MODE:      'SET_MESHGATE_MODE',
  SET_MESH_FIELD:         'SET_MESH_FIELD',
  SET_UPLINK_TYPE:        'SET_UPLINK_TYPE',
  SET_UPLINK_FIELD:       'SET_UPLINK_FIELD',
  SET_UPLINK_WIRELESS:    'SET_UPLINK_WIRELESS',
  SET_AP:                 'SET_AP',
  REMOVE_AP:              'REMOVE_AP',
  SET_ADMIN_PASSWORD:     'SET_ADMIN_PASSWORD',
  SET_ADMIN_PASSWORD_CONFIRM: 'SET_ADMIN_PASSWORD_CONFIRM',
  RESET:                  'RESET',
  HYDRATE_FROM_STATUS:    'HYDRATE_FROM_STATUS',
};

// reducer is exported for unit tests so they can drive it without a
// full Provider tree.
export function reducer(state, action) {
  switch (action.type) {
    case SETUP_ACTIONS.SET_HOSTNAME:
      return { ...state, hostname: action.value };

    case SETUP_ACTIONS.SET_ROLE: {
      // Role flip clears the orthogonal mode so the resulting profile
      // is consistent (handler validation rejects mixed mode/role).
      const next = { ...state, role: action.value };
      if (action.value === MeshRole.MESH_GATE) {
        next.meshpointMode = MeshPointMode.UNSPECIFIED;
        if (next.meshgateMode === MeshGateMode.UNSPECIFIED) {
          next.meshgateMode = MeshGateMode.ROUTER;
        }
      } else if (action.value === MeshRole.MESH_POINT) {
        next.meshgateMode = MeshGateMode.UNSPECIFIED;
        if (next.meshpointMode === MeshPointMode.UNSPECIFIED) {
          next.meshpointMode = MeshPointMode.EXTENDER;
        }
        // Mesh Point doesn't expose an uplink step.
        next.uplink = { ...initialState.uplink };
      }
      return next;
    }

    case SETUP_ACTIONS.SET_MESHPOINT_MODE:
      return { ...state, meshpointMode: action.value };

    case SETUP_ACTIONS.SET_MESHGATE_MODE:
      return { ...state, meshgateMode: action.value };

    case SETUP_ACTIONS.SET_MESH_FIELD:
      return { ...state, mesh: { ...state.mesh, [action.field]: action.value } };

    case SETUP_ACTIONS.SET_UPLINK_TYPE:
      return { ...state, uplink: { ...state.uplink, type: action.value } };

    case SETUP_ACTIONS.SET_UPLINK_FIELD:
      return { ...state, uplink: { ...state.uplink, [action.field]: action.value } };

    case SETUP_ACTIONS.SET_UPLINK_WIRELESS:
      return {
        ...state,
        uplink: {
          ...state.uplink,
          wireless: { ...state.uplink.wireless, [action.field]: action.value },
        },
      };

    case SETUP_ACTIONS.SET_AP: {
      // Insert or update by radioName. Action carries a partial AP profile
      // — fields not in `value` are merged with the existing entry, or
      // defaulted on insert.
      const idx = state.aps.findIndex(ap => ap.radioName === action.value.radioName);
      const baseDefaults = {
        radioName:  action.value.radioName,
        enabled:    false,
        ssid:       '',
        passphrase: '',
        encryption: WifiEncryption.PSK2,
      };
      const merged = idx >= 0
        ? { ...state.aps[idx], ...action.value }
        : { ...baseDefaults, ...action.value };
      const aps = [...state.aps];
      if (idx >= 0) aps[idx] = merged;
      else aps.push(merged);
      return { ...state, aps };
    }

    case SETUP_ACTIONS.REMOVE_AP:
      return { ...state, aps: state.aps.filter(ap => ap.radioName !== action.radioName) };

    case SETUP_ACTIONS.SET_ADMIN_PASSWORD:
      return { ...state, adminPassword: action.value };

    case SETUP_ACTIONS.SET_ADMIN_PASSWORD_CONFIRM:
      return { ...state, adminPasswordConfirm: action.value };

    case SETUP_ACTIONS.RESET:
      return { ...initialState };

    case SETUP_ACTIONS.HYDRATE_FROM_STATUS: {
      // Pre-fill the mesh radio with the first HaLow radio reported by
      // the backend so the user doesn't have to pick when there's an
      // obvious choice.
      const halow = (action.status?.radios ?? []).find(r => r.isHalow);
      if (!halow) return state;
      return { ...state, mesh: { ...state.mesh, radioName: halow.name } };
    }

    default:
      return state;
  }
}

const SetupContext = createContext(null);

export function SetupProvider({ children }) {
  const [state, dispatch] = useReducer(reducer, initialState);

  const value = useMemo(() => ({ state, dispatch }), [state]);

  return <SetupContext.Provider value={value}>{children}</SetupContext.Provider>;
}

export function useSetup() {
  const ctx = useContext(SetupContext);
  if (ctx === null) {
    throw new Error('useSetup must be called inside a <SetupProvider>');
  }
  return ctx;
}
