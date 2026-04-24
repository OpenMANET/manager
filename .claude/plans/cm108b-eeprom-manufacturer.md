# CM108B EEPROM write — idempotent Manufacturer string provisioning

## Context

The OpenVLM hardware uses a C-Media **CM108B** USB audio chip with an attached **93C46** SPI EEPROM (1 Kbit, 64 × 16-bit). The EEPROM holds the USB descriptor overrides (VID, PID, serial number, Product string, Manufacturer string, volume defaults). The default Manufacturer string shipped by C-Media is `C-Media Electronics Inc.` — we want it to read `OpenVLM` so the device self-identifies correctly in `lsusb`, udev, and downstream tooling. Detection of the chip is already implemented ([cm108.go](../../internal/comms/device/cm108.go) `DiscoverCM108`, [openvlm_identity.go](../../internal/comms/device/openvlm_identity.go) `CheckOpenVLMIdentity`). What's missing is the ability to **write** the EEPROM contents idempotently from the daemon.

## Answer: yes, possible — with one discovery gate

The CM108B datasheet §7.4 exposes the 93C46 through HID reports — no kernel driver, no vendor blob:

- **Set_Output_Report** (`bmRequestType=0x21, bRequest=0x09, wValue=0x0200, wIndex=3, wLength=4`) with `HID_OR0[7:6] = 0b10` routes the 4 output bytes to the chip's `EEPROM_DATA0`, `EEPROM_DATA1`, and `EEPROM_CTRL` latches.
- **Get_Input_Report** (`bmRequestType=0xA1, bRequest=0x01, wValue=0x0100, wIndex=3, wLength=4`) with `HID_OR0[7] = 1` maps `EEPROM_DATA0/1/CTRL` back through `HID_IR1/IR2/IR3`.
- Both are standard HID class requests — addressable on Linux via `HIDIOCSOUTPUT` and `HIDIOCGINPUT` ioctls. The `HIDIOCGINPUT` path is already proven in [openvlm_identity.go:79-103](../../internal/comms/device/openvlm_identity.go#L79-L103).

### The one gap

§7.4 names `EEPROM_CTRL` but does **not** document its bit layout — i.e. which bits encode the 6-bit 93C46 address, which bit selects read vs. write, and which bit strobes/reports busy. §7.1.3 documents the *content* at each EEPROM address but not the control byte that drives reads/writes. §7.1.4 documents the raw SPI timing between CM108B ↔ 93C46 but not the HID-side bridge.

The conventional decoding across public CM108-family implementations (Linux `hid-cm108` variants, direwolf GPIO code, AllStarLink `cm108eeprom` scripts) is:

- `EEPROM_CTRL[7]` = START (write) / BUSY (read-back)
- `EEPROM_CTRL[6]` = direction: `0` = read, `1` = write
- `EEPROM_CTRL[5:0]` = 6-bit word address (matches 93C46's 64-word space)
- 93C46 `EWEN`/`EWDS` issued via the special-opcode address range documented in §7.1.4's underlying Atmel 93C46 spec

This is the Phase 1 discovery gate — **we confirm the encoding on a live device with a read-only dump tool before any code writes to the chip**.

## Idempotency strategy

1. Read the full EEPROM image (64 words = 128 bytes). One `Get_Input_Report` per word; the whole dump is < 1 s even at 93C46's conservative timings.
2. Decode into a typed `EEPROMImage` struct (§7.1.3 layout).
3. Build a desired image from the daemon's configuration (initially: only `MagicWord = 0x670E`, `ManufacturerString = "OpenVLM"`; every other field marked "preserve").
4. Diff desired vs. current at the word level.
5. If diff is empty → **return, no writes**. If non-empty → `EWEN`, write only the dirty words, `EWDS`, re-read, verify.

The device re-reads its EEPROM only at USB reset / power-cycle. Write success takes effect on the next re-enumeration — we log that fact explicitly so an operator isn't surprised when `lsusb` still shows the old string right after boot.

## Manufacturer string encoding (target: "OpenVLM")

- 7 characters × 2 bytes UTF-16LE + 2-byte descriptor header = **16 bytes** ⇒ `bLength = 0x10`.
- Per §7.1.3 "1st byte (bit15-bit8, first character)" the chip treats the EEPROM word's **high byte** as the first character byte of the descriptor stream. Assembled layout:

    | Addr  | Hi byte           | Lo byte           |
    |-------|-------------------|-------------------|
    | 0x1A  | `'O'` (0x4F)      | `bLength` (0x10)  |
    | 0x1B  | `'p'` (0x70)      | 0x00              |
    | 0x1C  | `'e'` (0x65)      | 0x00              |
    | 0x1D  | `'n'` (0x6E)      | 0x00              |
    | 0x1E  | `'V'` (0x56)      | 0x00              |
    | 0x1F  | `'L'` (0x4C)      | 0x00              |
    | 0x20  | `'M'` (0x4D)      | 0x00              |
    | 0x21..0x29 | 0x00         | 0x00              |

  Validate during Phase 1 by reading the factory `C-Media Electronics Inc.` string and confirming the decoder round-trips it.

- Magic word (addr 0x00): write `0x670E` if not already valid. Bits: `[15:4]=0x670` magic, `[3]=1` volumes valid, `[2]=1` reserved, `[1]=1` serial enable, `[0]=1` reserved.

## Design — full descriptor overlay

Per-field `*T` pointers in a patch struct give clean "set this, preserve that" intent without ambiguity around zero values:

```go
// EEPROMImage is the full §7.1.3 decoded view of a 93C46 attached to a
// CM108B. All 64 words are represented. Used as both read-out and desired-
// state input to Ensure().
type EEPROMImage struct {
    MagicWord          uint16
    VID, PID           uint16
    SerialNumber       string
    ProductString      string
    ManufacturerString string
    DACInitVolumeDB    int8
    ADCInitVolumeDB    int8
    // ... remaining §7.1.3 fields
    Raw [64]uint16 // source of truth; decoders populate the typed fields
}

// EEPROMPatch expresses desired state. Nil pointer = "preserve current".
type EEPROMPatch struct {
    MagicWord          *uint16
    ManufacturerString *string
    ProductString      *string
    VID, PID           *uint16
    // ...
}
```

Public API:

```go
// HIDTransport is the test seam — production uses the ioctl-backed impl.
type HIDTransport interface {
    SetOutputReport(hidPath string, or0, or1, or2, or3 byte) error
    GetInputReport(hidPath string)  (ir0, ir1, ir2, ir3 byte, err error)
}

func ReadEEPROMImage(d CM108Descriptor, t HIDTransport) (EEPROMImage, error)
func EnsureEEPROM(d CM108Descriptor, patch EEPROMPatch, t HIDTransport) (changed bool, err error)
```

First consumer — and the only one wired to startup in this change:

```go
manufacturer := "OpenVLM"
magic := uint16(0x670E)
_, err := device.EnsureEEPROM(d, device.EEPROMPatch{
    MagicWord:          &magic,
    ManufacturerString: &manufacturer,
}, nil) // nil ⇒ default ioctl transport
```

## Phased rollout

**Phase 1 — read-only probe** (no hardware risk)
- `internal/comms/device/eeprom.go`: `HIDTransport`, `ioctlTransport` backed by `HIDIOCSOUTPUT`/`HIDIOCGINPUT`, `readWord`, `ReadEEPROMImage`.
- Add `cmd/openmanetd-cm108-dump` (or a `-tags hardware` helper) to print the 64-word image for an attached device.
- **Phase 1 gate:** run the dump against a factory OpenVLM unit. Confirm: magic word `0x670X`, Manufacturer string decodes to `C-Media Electronics Inc.`, `EEPROM_CTRL[7]` clears as expected after reads. If any of that fails, we don't proceed to Phase 2 — we fix the protocol understanding first.

**Phase 2 — idempotent write**
- Add `writeWord`, `ewen`, `ewds`, `EnsureEEPROM`.
- Bounded retries (max 3) on post-write verify; fail loud if verify still mismatches.
- Guarded by positive `IsOpenVLM` identity — never writes to a generic CM108 dongle attached for debugging.

**Phase 3 — startup wiring**
- `internal/openmanet/openmanet.go`: after `DiscoverCM108` + `CheckOpenVLMIdentity`, call `EnsureEEPROM` per OpenVLM device with the Manufacturer patch.
- Non-fatal: log at `Warn` on failure, continue daemon startup.
- On `changed == true`, log at `Info`: `"CM108B EEPROM updated; new Manufacturer string will appear after next USB re-enumeration"`.

## Files to add / modify

- **New** `internal/comms/device/eeprom.go` — `HIDTransport` seam, ioctl-backed transport (factors out the `HIDIOCGINPUT` ioctl from `openvlm_identity.go` plus a symmetric `HIDIOCSOUTPUT`), `EEPROMImage`, `EEPROMPatch`, read/write word primitives, 93C46 `EWEN`/`EWDS`, `ReadEEPROMImage`, `EnsureEEPROM`, string encode/decode per §7.1.3.
- **New** `internal/comms/device/eeprom_test.go` — hand-written fake `HIDTransport` with programmable 64-word memory. Tests: (a) first-run writes expected words, (b) second-run issues zero `SetOutputReport` calls with `HID_OR0[6]=1`, (c) stale magic word triggers a magic-word write, (d) verify mismatch surfaces an error after 3 retries, (e) encode/decode round-trip for factory and target strings.
- **New** `cmd/openmanetd/cm108_dump.go` (Cobra subcommand, e.g. `openmanetd debug cm108-dump`) — prints the decoded `EEPROMImage` plus a hex table of `Raw`. Used during Phase 1 validation; kept afterwards as a diagnostic.
- **Modify** [openvlm_identity.go](../../internal/comms/device/openvlm_identity.go) — extract the ioctl number constants + opener so `eeprom.go` doesn't duplicate the `hidIOCGInput` encoding. Preserve `CheckOpenVLMIdentity`'s public signature.
- **Modify** [internal/openmanet/openmanet.go](../../internal/openmanet/openmanet.go) — invoke `EnsureEEPROM` once per OpenVLM-identified device at startup (Phase 3 only).
- **Do not modify** [cm108.go](../../internal/comms/device/cm108.go) discovery — already correct.

## Verification

1. **`make test`** / **`make test-race`** — unit tests all green; no data races in the concurrent-access tests.
2. **Phase 1 bench gate** — `openmanetd debug cm108-dump` against a factory OpenVLM device decodes the advertised Manufacturer and confirms our `EEPROM_CTRL` encoding is correct. This is a hard gate: Phase 2 does not merge until Phase 1 validates.
3. **Phase 2 bench test** — on the same device, run `openmanetd debug cm108-ensure --manufacturer OpenVLM` (or equivalent), power-cycle the USB port, `lsusb -v` shows `OpenVLM`. Re-run the command — no `Write` log lines. Dump — magic word still valid, all other fields unchanged.
4. **Phase 3 end-to-end** — fresh factory device → daemon boots → operator sees `"EEPROM updated; ... next USB re-enumeration"` log once → re-plug → `lsusb -v` shows `OpenVLM` → second daemon boot is silent on the EEPROM path.
5. **Regression** — boot daemon against a non-OpenVLM CM108 dongle (generic USB headset): `CheckOpenVLMIdentity` returns false, `EnsureEEPROM` is not called, zero writes occur.
