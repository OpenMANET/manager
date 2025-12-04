# OpenMANET Manager - AI Coding Agent Instructions

## Project Overview
OpenMANETd is a Go daemon that manages low-level configurations for OpenMANET mesh networks. It runs on OpenWrt routers, coordinating B.A.T.M.A.N.-Advanced mesh networking, UCI configuration management, Alfred data distribution, and Push-to-Talk (PTT) audio over the mesh.

## Architecture

### Core Components
1. **Management Layer** (`internal/mgmt/`) - Orchestrates Alfred data workers that periodically exchange network state
   - `gateway.go` - Publishes/receives gateway availability (60s send, 10s receive intervals)
   - `node.go` - Publishes/receives node information (60s intervals)
   - `address_reservation.go` - DHCP address coordination (4s send, 10s receive)
   - Workers use the go-alfred client library (local module in `internal/alfred/`)

2. **Network Configuration** (`internal/network/`) - UCI (Unified Configuration Interface) abstraction
   - OpenWrt uses UCI for system configuration via text files in `/etc/config/`
   - `uci_network.go` - Bridge/interface configuration (br-ahwlan, bat0)
   - `uci_dhcp.go` - DHCP server/client settings
   - `uci_openmanet.go` - Custom OpenMANET configuration
   - All UCI operations use the `digineo/go-uci/v2` library with `ConfigReader` interface for testability

3. **B.A.T.M.A.N.-Advanced Integration** (`internal/batman-adv/`) - Mesh protocol operations
   - Uses `batctl` CLI tool (not Go bindings) via `exec.Command`
   - `mesh_config.go` - Parses `batctl mj` JSON output for gateway mode detection
   - `gateway_config.go` - Configures gateway server/client modes
   - `hosts.go` - Manages `/etc/bat-hosts` for hostname resolution

4. **PTT System** (`internal/ptt/`) - Real-time audio communication
   - Opus codec (48kHz, 12kbps) over multicast UDP (default: 224.0.0.1:5007)
   - Uses PortAudio for audio I/O and `gvalkov/golang-evdev` for USB HID PTT devices
   - Integrates with AIOC (All-In-One-Cable) USB soundcard firmware
   - AIOC provides USB Audio (48kHz/16-bit), HID interface (/dev/hidraw*), and serial (/dev/ttyACM0)
   - PTT button monitoring via HID interface (CM108-compatible events)
   - Push-to-talk mode: Hold button to transmit, release to stop
   - Device detection supports exact match, partial match, and AIOC auto-detection
   - Runs only when `ptt.enable: true` in config

5. **Protobuf API** (`internal/api/openmanet/v1/`) - Generated from git submodule in `proto/`
   - Uses vtprotobuf for efficient marshaling (see `buf.gen.yaml`)
   - Data types: Gateway, Node, Position (GPS), AddressReservation
   - Generate with: `buf generate` (requires protobuf submodule: `git submodule update --init`)

### Data Flow
```
Config Change (fsnotify) → Config.reload() → Worker notification
                                               ↓
                                          Workers adjust intervals
                                               ↓
Alfred Client ← Workers (periodic) → Protobuf serialize → Alfred data types (64, 65, etc.)
       ↓
B.A.T.M.A.N.-Advanced mesh → Neighbor nodes
```

## Development Workflow

### Build & Run
```bash
make build      # CGO_ENABLED=1, outputs to bin/openmanetd
make run        # go run with fmt/vet
make test       # Standard go test
make alfred     # Build C bindings in internal/alfred/alfred/
```

### Protobuf Generation
```bash
buf generate    # Requires proto/ submodule populated
# Outputs to internal/api/ with vtprotobuf, connectrpc plugins
```

### Cross-Compilation for OpenWrt
Uses goreleaser-cross Docker image with custom sysroots:
```bash
make sysroot-unpack      # Extracts pre-built OpenWrt toolchains
make release-dry-run     # Test build without publishing
make release             # Requires .release-env file
```

### Configuration
- Default config path: `/etc/openmanet/config.yml` (overridable via `--config`)
- Uses Viper with fsnotify for hot-reload
- See `example_config.yml` for all options
- Config struct in `internal/config/config.go` provides thread-safe getters (sync.RWMutex)

## Code Conventions

### Testing Patterns
- UCI operations use `ConfigReader` interface for mocking (see `*_test.go` files)
- Test fixtures in `testfixtures/uci/` contain realistic OpenWrt configs
- Batman-adv functions mock `batctl` calls (see `mesh_config_test.go`)

### Error Handling
- Use zerolog for structured logging: `log.Error().Err(err).Msg("context")`
- Workers continue on error rather than panic (mesh networks are unreliable)
- UCI commit failures are logged but don't crash the daemon

### Naming Conventions
- UCI section names follow OpenWrt conventions: `lan`, `wan`, `ahwlan` (ad-hoc WLAN)
- Alfred data types use uint8 constants (64=Gateway, 65=Node, etc.)
- Interface names: `br-ahwlan` (bridge), `bat0` (B.A.T.M.A.N. interface)

### Worker Pattern
All management workers follow this structure:
1. `NewXWorker()` constructor with config validation
2. `StartSend()` - Ticker-based publishing loop with shutdown channel
3. `StartReceive()` - Ticker-based subscription loop
4. Early exit when interface not configured (avoids spamming logs)

### CGO Dependencies
- `gordonklaus/portaudio` - Audio I/O (requires libportaudio-dev)
- `internal/alfred` - C bindings to Alfred daemon (replaced module with local path)
- Build with `CGO_ENABLED=1` (see Makefile)

## Critical Details

### B.A.T.M.A.N.-Advanced Context
- Gateway selection uses MAC addresses from `bat0` interface, not bridge MAC
- The daemon clears `/etc/bat-hosts` on startup to prevent stale name resolution
- Gateway bandwidth is in kbps (configured via `batctl gw_mode server 10000/2000`)
- Mesh config cached for 60s to avoid excessive `batctl` calls

### UCI Configuration Quirks
- Options can be single-value or multi-value (list) - check with `GetType()`
- Changes require `tree.Commit()` followed by system service reload
- Config files may not exist initially (OpenWrt creates on first write)
- Always validate section existence before modifying options

### Alfred Protocol
- Socket path: `/var/run/alfred.sock` (Unix domain socket)
- Data types are arbitrary uint8 values (coordinate with Alfred daemon config)
- Version field allows schema evolution (currently all version 1)
- Request timeout is 5s (hardcoded in go-alfred library)

### DevContainer Support
The project includes a devcontainer with all dependencies. When adding new system dependencies, update `.devcontainer/devcontainer.json`.

### AIOC USB Soundcard Integration
- **Hardware**: AIOC (All-In-One-Cable) USB device with STM32 firmware
- **Interfaces**:
  - USB Audio Class: 48kHz, 16-bit mono, mic input + speaker output
  - HID Interface (`/dev/hidraw*`): CM108-compatible button events for PTT
  - Serial Interface (`/dev/ttyACM0`): Optional PTT via DTR/RTS signals
- **PTT Operation**:
  - Push-to-talk mode: Hold button to transmit, release to stop
  - Button events filtered by `pttKey` config (default: "any" accepts all buttons)
  - Device detection: supports exact match, partial match ("AIOC", "All-In-One-Cable")
- **Audio Flow**:
  - TX: Mic → PortAudio → Opus encode → UDP multicast
  - RX: UDP multicast → Opus decode → PortAudio → Speaker
- **Firmware**: https://github.com/skuep/AIOC (reference for HID protocol details)
- **Debugging**: Set `ptt.debug: true` to see available HID devices on startup

## Common Tasks

### Adding a New UCI Configuration Field
1. Define constant in `internal/network/uci_*.go`
2. Add struct field with uci tags: `` `uci:"option myfield"` ``
3. Add getter to `ConfigReader` interface
4. Update test fixtures in `testfixtures/uci/`

### Adding a New Alfred Data Type
1. Define protobuf message in `proto/` submodule
2. Run `buf generate` to update Go bindings
3. Create worker in `internal/mgmt/` following existing patterns
4. Register in `mgmt.go` Start() method with interval tuning
5. Update `example_config.yml` with new dataTypes option

### Debugging Mesh Issues
- Check B.A.T.M.A.N. status: `batctl if` and `batctl n`
- Verify Alfred daemon: `alfred -r 64` (request data type 64)
- Monitor UCI: `uci show network` and `uci show dhcp`
- Enable debug logging: Set `logLevel: debug` in config

## Dependencies of Note
- `vishvananda/netlink` - Low-level network interface manipulation
- `spf13/cobra` + `viper` - CLI and configuration (standard pattern)
- `planetscale/vtprotobuf` - Faster protobuf marshaling than standard library
- `rs/zerolog` - Zero-allocation JSON logger
