module github.com/openmanet/openmanetd

go 1.23.0

require (
	github.com/common-nighthawk/go-figure v0.0.0-20210622060536-734e95fb86be
	github.com/digineo/go-uci/v2 v2.0.0-20231120164223-60c14814b8fe
	github.com/gordonklaus/portaudio v0.0.0-20250206071425-98a94950218b
	github.com/gvalkov/golang-evdev v0.0.0-20220815104727-7e27d6ce89b6
	github.com/hraban/opus v0.0.0-20230925203106-0188a62cb302
	github.com/openmanet/go-alfred v0.0.0-202404291200151-8f3f3f4e2f4e
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10
	github.com/rs/zerolog v1.34.0
	github.com/spf13/cobra v1.10.1
	github.com/spf13/viper v1.21.0
	github.com/vishvananda/netlink v1.3.1
	golang.org/x/net v0.33.0
	golang.org/x/sys v0.29.0
	google.golang.org/protobuf v1.36.1
)

require (
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/text v0.28.0 // indirect
)

replace github.com/openmanet/go-alfred => ./internal/alfred
