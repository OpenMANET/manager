# Setup wizard bench checklist

These are the setup-wizard behaviours CI cannot prove — every I/O seam in the
wizard and the reservation worker is faked or injected in tests, so the runners
never touch real radios, Alfred propagation, `netifd`, or a reboot. Confirm
these on hardware before treating the corresponding wizard-parity phase as fully
done. None of them is a CI acceptance criterion; they are field checks.

## P1 — LuCI hand-off and re-run

- After a Go-wizard apply on a real image, LuCI no longer shows the Morse
  landing page (homepage cleared, `luci.wizard.used=1`).
- A device that reserved a mesh address before re-reserves after a wizard re-run
  (`setup-reset` clears `dhcpconfigured`).
- A `sae-mixed` AP accepts both a WPA2 and a WPA3 client.

## P4 — batmesh1 ownership (MT7915/MT7916 boards)

- With the 2.4 GHz AP enabled, the daemon leaves `default_<radio>` alone on
  first boot (no silent AP→mesh conversion).
- With a mesh backhaul chosen, the radio comes up in mesh mode on channel 8 /
  HE20 under `batmesh1_<radio>`.
- The radio's iwinfo hardware name resolves at wizard time on a factory image so
  the wizard offers "mesh backhaul" only where the daemon would accept it.

## P5 — address reservation

- The post-reservation reboot is still required — confirm the in-place network
  reload alone does not wedge the mesh/alfred/batman interfaces.
- Alfred propagation latency between a peer's `Set` and this node's `Request` is
  within the 60 s / 125 s worker cadence.
- Two fresh gates that both take `10.41.0.1` converge after one extra reboot of
  the higher-MAC node (derived from the allocator and the MAC tie-break, not
  reproduced on hardware).

## P6 — transport MTU (do these before removing the daemon's netlink pass)

0. Before the first wizard run on each board, record
   `uci show network | grep -E "mtu|device"` — the reset strips `mtu` only from
   `br-ahwlan` and detected ethernet ports now, so an unrelated vendor `mtu`
   (e.g. on `br-lan`) must survive.
1. Run the wizard once as a gate (router on eth, uplink eth0) and once as a
   point extender. `uci show network | grep -E "mtu|wizard_device"` must show
   `mtu='1460'` on the `br-ahwlan` section and one section per bridged ethernet
   port, none on the uplink port, none for `bat0`.
2. `service openmanetd stop; reload_config; ip link show br-ahwlan; ip link show eth1`
   (and each bridged port). Expect `mtu 1460` from UCI alone. If a bridged port
   still shows 1500 while the bridge shows 1460, the per-port sections are
   load-bearing; if the ports show 1460 even after deleting their sections,
   `netifd` propagates the bridge MTU and the per-port sections are redundant —
   record which.
3. `service openmanetd start; logread -e "Set MTU"`. Expect no "Set MTU for
   bridge interface" / "Set MTU for Ethernet interface" lines for the bridged
   ports (the change-only compare found 1460 already applied); "Set MTU for mesh
   interface" lines still appear (wlan ifaces stay netlink-only).
4. Re-run the wizard with the other port as uplink and repeat 1–3. The former
   bridged port must have no `mtu` in UCI afterward, and no stale
   `wizard_device_*` section may remain.
5. Only after 1–3 hold on both scenarios may a later phase remove the
   bridge/ethernet netlink writes in `internal/mgmt/device.go`.
