module github.com/openmanet/openmanetd

go 1.26.0

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.11-20260209202127-80ab13bee0bf.1
	connectrpc.com/connect v1.19.1
	connectrpc.com/cors v0.1.0
	github.com/common-nighthawk/go-figure v0.0.0-20210622060536-734e95fb86be
	github.com/coreywagehoft/go-tak v1.2.0
	github.com/digineo/go-uci/v2 v2.0.0-20231120164223-60c14814b8fe
	github.com/fsnotify/fsnotify v1.9.0
	github.com/gordonklaus/portaudio v0.0.0-20250206071425-98a94950218b
	github.com/gvalkov/golang-evdev v0.0.0-20220815104727-7e27d6ce89b6
	github.com/hraban/opus v0.0.0-20230925203106-0188a62cb302
	github.com/mattn/go-sqlite3 v1.14.34
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/mdlayher/wifi v0.7.2
	github.com/openmanet/go-alfred v0.0.0-202404291200151-8f3f3f4e2f4e
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10
	github.com/rs/cors v1.11.1
	github.com/rs/zerolog v1.34.0
	github.com/spf13/cobra v1.10.2
	github.com/spf13/viper v1.21.0
	github.com/vishvananda/netlink v1.3.1
	golang.org/x/net v0.51.0
	golang.org/x/sys v0.42.0
	google.golang.org/grpc v1.79.2
	google.golang.org/protobuf v1.36.11
	tailscale.com v1.96.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/akutz/memconn v0.1.0 // indirect
	github.com/coder/websocket v1.8.12 // indirect
	github.com/dblohm7/wingoes v0.0.0-20240119213807-a09d6be7affa // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20250813024750-ebf49471dced // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hdevalence/ed25519consensus v0.2.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/jsimonetti/rtnetlink v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/genetlink v1.3.2 // indirect
	github.com/mdlayher/netlink v1.8.0 // indirect
	github.com/mdlayher/packet v1.1.2 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/mitchellh/go-ps v1.0.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tailscale/go-winio v0.0.0-20231025203758-c4f33415bf55 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	go4.org/mem v0.0.0-20240501181205-ae6ca9944745 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/exp v0.0.0-20251023183803-a4bb9ffd2546 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.zx2c4.com/wireguard/windows v0.5.3 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251222181119-0a764e51fe1b // indirect
)

replace github.com/openmanet/go-alfred => ./internal/alfred
