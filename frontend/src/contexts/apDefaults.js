import { WifiEncryption } from '../gen/openmanet/wifi_config/v1/wifi_config_pb.js';

// apDefaults is the per-radio entry shape in state.aps. `enabled` is
// the client-AP switch; `meshBackhaul` marks the one radio (at most)
// that runs the 2.4 GHz batman-adv backhaul instead, with its own
// mesh ID and passphrase.
//
// Lives outside SetupContext.jsx so that file only exports components
// and hooks — a non-component export there breaks React Fast Refresh.
export function apDefaults(radioName) {
  return {
    radioName,
    enabled:            false,
    ssid:               '',
    passphrase:         '',
    encryption:         WifiEncryption.PSK2,
    meshBackhaul:       false,
    backhaulMeshId:     '',
    backhaulPassphrase: '',
  };
}
