package config

import (
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Default configuration values
const (
	DefaultMeshNetInterface                          string  = "br-ahwlan"
	DefaultDBFile                                    string  = "/etc/openmanetd/openmanetd.db"
	DefaultAlfredMode                                string  = "primary"
	DefaultAlfredBatInterface                        string  = "bat0"
	DefaultBatmanMulticastEnhancementsEnabled        bool    = true
	DefaultAlfredSocketPath                          string  = "/var/run/alfred.sock"
	DefaultAlfredEnable                              bool    = true
	DefaultAlfredDataTypeGateway                     bool    = true
	DefaultAlfredDataTypeNode                        bool    = true
	DefaultAlfredDataTypePosition                    bool    = true
	DefaultAlfredDataTypeAddressReserv               bool    = true
	DefaultCommsEnable                               bool    = false
	DefaultCommsProtocol                             string  = "rtp"
	DefaultCommsDebug                                bool    = false
	DefaultCommsLoopback                             bool    = false
	DefaultCommsTrace                                bool    = false
	DefaultCommsControlSource                        string  = "openvlm"
	DefaultCommsPlaybackBuffer                       int     = 2
	DefaultCommsMicGain                              float32 = 1.0
	DefaultCommsNanoPTTEnable                        bool    = false
	DefaultCommsNanoPTTDevicePath                    string  = "/dev/hidraw0/*"
	DefaultCommsNanoPTTDeviceName                    string  = ""
	DefaultCommsBluetoothPttEnable                   bool    = false
	DefaultCommsBluetoothPttBluetoothAudioDeviceHint string  = ""
	DefaultCommsBluetoothPttBluetoothInputDevice     string  = ""
	DefaultCommsBluetoothPttBluetoothOutputDevice    string  = ""
	DefaultResetDBOnStart                            bool    = false
	DefaultEnableGNSS                                bool    = false
	DefaultGNSSSendAsNMEA                            bool    = false
	DefaultGNSSSendAsCoT                             bool    = false
	DefaultEnableBLOS                                bool    = false
	DefaultBLOSStatusWorkerInterval                  int     = 30 // seconds
	DefaultOpenMANETFrontendURL                      string  = "http://localhost:8081"
	DefaultOpenMANETWebsocketPort                    int     = 0
	DefaultOpenMANETAPIAddress                       string  = "http://0.0.0.0:8087"
)

// Config holds the application configuration values with automatic reloading support.
type Config struct {
	v                                         *viper.Viper
	CommsNanoPTTDeviceName                    string
	CommsBluetoothPttBluetoothAudioDeviceHint string
	AlfredMode                                string
	AlfredBatInterface                        string
	AlfredSocketPath                          string
	MeshNetInterface                          string
	CommsNanoPTTDevicePath                    string
	CommsBluetoothPttBluetoothOutputDevice    string
	DBFile                                    string
	CommsControlSource                        string
	CommsProtocol                             string
	CommsBluetoothPttBluetoothInputDevice     string
	OpenMANETFrontendURL                      string
	OpenMANETAPIAddress                       string
	onChangeCallbacks                         []func(*Config)
	CommsPlaybackBuffer                       int
	BLOSStatusWorkerInterval                  int
	OpenMANETWebsocketPort                    int
	mu                                        sync.RWMutex
	CommsMicGain                              float32
	BLOSEnable                                bool
	CommsLoopback                             bool
	AlfredDataTypeGateway                     bool
	AlfredEnable                              bool
	AlfredDataTypePosition                    bool
	AlfredDataTypeAddressReserv               bool
	AlfredDataTypeNode                        bool
	BatmanMulticastEnhancementsEnabled        bool
	CommsDebug                                bool
	CommsEnable                               bool
	CommsTrace                                bool
	CommsNanoPTTEnable                        bool
	CommsBluetoothPttEnable                   bool
	ResetDBOnStart                            bool
	EnableGNSS                                bool
	GNSSSendAsNMEA                            bool
	GNSSSendAsCoT                             bool
}

// New creates a new Config instance with the given viper instance.
// If v is nil, uses the global viper instance.
// It loads the initial configuration and sets up automatic reloading.
func New(v *viper.Viper) *Config {
	if v == nil {
		v = viper.GetViper()
	}

	c := &Config{
		v:                 v,
		onChangeCallbacks: make([]func(*Config), 0),
	}

	// Load initial configuration
	c.reload()

	// Set up automatic config reloading
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		c.reload()
		c.notifyCallbacks()
	})

	return c
}

// NewWithoutWatch creates a new Config instance without starting the file
// watcher. This is useful for tests where fsnotify would cause race conditions.
func NewWithoutWatch(v *viper.Viper) *Config {
	if v == nil {
		v = viper.GetViper()
	}

	c := &Config{
		v:                 v,
		onChangeCallbacks: make([]func(*Config), 0),
	}

	c.reload()

	return c
}

// reload reads all configuration values from viper and updates the Config fields.
func (c *Config) reload() { //nolint:gocognit,gocyclo
	c.mu.Lock()
	defer c.mu.Unlock()

	// Load mesh network configuration
	if val := c.v.GetString("meshNetInterface"); val != "" {
		c.MeshNetInterface = val
	} else {
		c.MeshNetInterface = DefaultMeshNetInterface
	}

	if val := c.v.GetString("dbFile"); val != "" {
		c.DBFile = val
	} else {
		c.DBFile = DefaultDBFile
	}

	if c.v.IsSet("resetDBOnStart") {
		c.ResetDBOnStart = c.v.GetBool("resetDBOnStart")
	} else {
		c.ResetDBOnStart = DefaultResetDBOnStart
	}

	if c.v.IsSet("gnss.enable") {
		c.EnableGNSS = c.v.GetBool("gnss.enable")
	} else {
		c.EnableGNSS = DefaultEnableGNSS
	}

	if c.v.IsSet("gnss.sendAsExternalGNSSSource.sendAsNMEA") {
		c.GNSSSendAsNMEA = c.v.GetBool("gnss.sendAsExternalGNSSSource.sendAsNMEA")
	} else {
		c.GNSSSendAsNMEA = DefaultGNSSSendAsNMEA
	}

	if c.v.IsSet("gnss.sendAsExternalGNSSSource.sendAsCoT") {
		c.GNSSSendAsCoT = c.v.GetBool("gnss.sendAsExternalGNSSSource.sendAsCoT")
	} else {
		c.GNSSSendAsCoT = DefaultGNSSSendAsCoT
	}

	if c.v.IsSet("batman.multicastEnhancementsEnabled") {
		c.BatmanMulticastEnhancementsEnabled = c.v.GetBool("batman.multicastEnhancementsEnabled")
	} else {
		c.BatmanMulticastEnhancementsEnabled = DefaultBatmanMulticastEnhancementsEnabled
	}

	// Load Alfred configuration
	if val := c.v.GetString("alfred.mode"); val != "" {
		c.AlfredMode = val
	} else {
		c.AlfredMode = DefaultAlfredMode
	}

	if val := c.v.GetString("alfred.batInterface"); val != "" {
		c.AlfredBatInterface = val
	} else {
		c.AlfredBatInterface = DefaultAlfredBatInterface
	}

	if val := c.v.GetString("alfred.socketPath"); val != "" {
		c.AlfredSocketPath = val
	} else {
		c.AlfredSocketPath = DefaultAlfredSocketPath
	}

	if c.v.IsSet("alfred.enable") {
		c.AlfredEnable = c.v.GetBool("alfred.enable")
	} else {
		c.AlfredEnable = DefaultAlfredEnable
	}

	// Load Alfred data type configuration
	if c.v.IsSet("alfred.dataTypes.gateway") {
		c.AlfredDataTypeGateway = c.v.GetBool("alfred.dataTypes.gateway")
	} else {
		c.AlfredDataTypeGateway = DefaultAlfredDataTypeGateway
	}

	if c.v.IsSet("alfred.dataTypes.node") {
		c.AlfredDataTypeNode = c.v.GetBool("alfred.dataTypes.node")
	} else {
		c.AlfredDataTypeNode = DefaultAlfredDataTypeNode
	}

	if c.v.IsSet("alfred.dataTypes.position") {
		c.AlfredDataTypePosition = c.v.GetBool("alfred.dataTypes.position")
	} else {
		c.AlfredDataTypePosition = DefaultAlfredDataTypePosition
	}

	if c.v.IsSet("alfred.dataTypes.addressReservation") {
		c.AlfredDataTypeAddressReserv = c.v.GetBool("alfred.dataTypes.addressReservation")
	} else {
		c.AlfredDataTypeAddressReserv = DefaultAlfredDataTypeAddressReserv
	}

	// Load comms configuration
	if c.v.IsSet("comms.enable") {
		c.CommsEnable = c.v.GetBool("comms.enable")
	} else {
		c.CommsEnable = DefaultCommsEnable
	}

	if val := strings.ToLower(c.v.GetString("comms.protocol")); val != "" {
		c.CommsProtocol = val
	} else {
		c.CommsProtocol = DefaultCommsProtocol
	}

	if c.v.IsSet("comms.debug") {
		c.CommsDebug = c.v.GetBool("comms.debug")
	} else {
		c.CommsDebug = DefaultCommsDebug
	}

	if c.v.IsSet("comms.loopback") {
		c.CommsLoopback = c.v.GetBool("comms.loopback")
	} else {
		c.CommsLoopback = DefaultCommsLoopback
	}

	if c.v.IsSet("comms.trace") {
		c.CommsTrace = c.v.GetBool("comms.trace")
	} else {
		c.CommsTrace = DefaultCommsTrace
	}

	if val := strings.ToLower(c.v.GetString("comms.controlSource")); val != "" {
		c.CommsControlSource = val
	} else {
		c.CommsControlSource = DefaultCommsControlSource
	}

	if val := c.v.GetInt("comms.playbackBuffer"); val > 0 {
		c.CommsPlaybackBuffer = val
	} else {
		c.CommsPlaybackBuffer = DefaultCommsPlaybackBuffer
	}

	if val := c.v.GetFloat64("comms.micGain"); val > 0 {
		c.CommsMicGain = float32(val)
	} else {
		c.CommsMicGain = DefaultCommsMicGain
	}

	// Load nanoPTT configuration
	if c.v.IsSet("comms.nanoPTT.enable") {
		c.CommsNanoPTTEnable = c.v.GetBool("comms.nanoPTT.enable")
	} else {
		c.CommsNanoPTTEnable = DefaultCommsNanoPTTEnable
	}

	if val := c.v.GetString("comms.nanoPTT.devicePath"); val != "" {
		c.CommsNanoPTTDevicePath = val
	} else {
		c.CommsNanoPTTDevicePath = DefaultCommsNanoPTTDevicePath
	}

	if val := c.v.GetString("comms.nanoPTT.deviceName"); val != "" {
		c.CommsNanoPTTDeviceName = val
	} else {
		c.CommsNanoPTTDeviceName = DefaultCommsNanoPTTDeviceName
	}

	// Load bluetoothPtt configuration
	if c.v.IsSet("comms.bluetoothPtt.enable") {
		c.CommsBluetoothPttEnable = c.v.GetBool("comms.bluetoothPtt.enable")
	} else {
		c.CommsBluetoothPttEnable = DefaultCommsBluetoothPttEnable
	}

	if val := c.v.GetString("comms.bluetoothPtt.BluetoothAudioDeviceHint"); val != "" {
		c.CommsBluetoothPttBluetoothAudioDeviceHint = val
	} else {
		c.CommsBluetoothPttBluetoothAudioDeviceHint = DefaultCommsBluetoothPttBluetoothAudioDeviceHint
	}

	if val := c.v.GetString("comms.bluetoothPtt.BluetoothInputDevice"); val != "" {
		c.CommsBluetoothPttBluetoothInputDevice = val
	} else {
		c.CommsBluetoothPttBluetoothInputDevice = DefaultCommsBluetoothPttBluetoothInputDevice
	}

	if val := c.v.GetString("comms.bluetoothPtt.BluetoothOutputDevice"); val != "" {
		c.CommsBluetoothPttBluetoothOutputDevice = val
	} else {
		c.CommsBluetoothPttBluetoothOutputDevice = DefaultCommsBluetoothPttBluetoothOutputDevice
	}

	// Load BLOS configuration
	if c.v.IsSet("blos.enable") {
		c.BLOSEnable = c.v.GetBool("blos.enable")
	} else {
		c.BLOSEnable = DefaultEnableBLOS
	}

	if val := c.v.GetInt("blos.statusWorkerInterval"); val != 0 {
		c.BLOSStatusWorkerInterval = val
	} else {
		c.BLOSStatusWorkerInterval = DefaultBLOSStatusWorkerInterval
	}

	if val := c.v.GetString("openmanetFrontendURL"); val != "" {
		c.OpenMANETFrontendURL = val
	} else {
		c.OpenMANETFrontendURL = DefaultOpenMANETFrontendURL
	}

	if val := c.v.GetString("openmanetAPIAddress"); val != "" {
		c.OpenMANETAPIAddress = val
	} else {
		c.OpenMANETAPIAddress = DefaultOpenMANETAPIAddress
	}

	if val := c.v.GetInt("openmanetWebsocketPort"); val != 0 {
		c.OpenMANETWebsocketPort = val
	} else {
		c.OpenMANETWebsocketPort = DefaultOpenMANETWebsocketPort
	}
}

// OnConfigChange registers a callback function to be called when the configuration changes.
func (c *Config) OnConfigChange(callback func(*Config)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.onChangeCallbacks = append(c.onChangeCallbacks, callback)
}

// notifyCallbacks calls all registered callback functions.
func (c *Config) notifyCallbacks() {
	c.mu.RLock()
	callbacks := make([]func(*Config), len(c.onChangeCallbacks))
	copy(callbacks, c.onChangeCallbacks)
	c.mu.RUnlock()

	for _, callback := range callbacks {
		callback(c)
	}
}

// GetMeshNetInterface returns the mesh network interface name.
func (c *Config) GetMeshNetInterface() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.MeshNetInterface
}

// GetDBFile returns the database file path.
func (c *Config) GetDBFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.DBFile
}

// GetResetDBOnStart returns whether to reset the database on start.
func (c *Config) GetResetDBOnStart() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.ResetDBOnStart
}

// GetEnableBatmanMulticastEnhancements returns whether batman-adv multicast enhancements are enabled.
func (c *Config) GetEnableBatmanMulticastEnhancements() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.BatmanMulticastEnhancementsEnabled
}

// GetEnableBLOS returns whether BLOS is enabled.
func (c *Config) GetEnableBLOS() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.BLOSEnable
}

// GetAlfredMode returns the Alfred operating mode (primary/secondary).
func (c *Config) GetAlfredMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AlfredMode
}

// GetAlfredBatInterface returns the batman-adv interface name for Alfred.
func (c *Config) GetAlfredBatInterface() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AlfredBatInterface
}

// GetAlfredSocketPath returns the Alfred socket path.
func (c *Config) GetAlfredSocketPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AlfredSocketPath
}

// GetAlfredEnable returns whether Alfred integration is enabled.
func (c *Config) GetAlfredEnable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AlfredEnable
}

// GetAlfredDataTypeGateway returns whether gateway data type is enabled.
func (c *Config) GetAlfredDataTypeGateway() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AlfredDataTypeGateway
}

// GetAlfredDataTypeNode returns whether node data type is enabled.
func (c *Config) GetAlfredDataTypeNode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AlfredDataTypeNode
}

// GetAlfredDataTypePosition returns whether position data type is enabled.
func (c *Config) GetAlfredDataTypePosition() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AlfredDataTypePosition
}

// GetAlfredDataTypeAddressReservation returns whether address reservation data type is enabled.
func (c *Config) GetAlfredDataTypeAddressReservation() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AlfredDataTypeAddressReserv
}

// GetCommsEnable returns whether the comms subsystem is enabled.
func (c *Config) GetCommsEnable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsEnable
}

// GetCommsProtocol returns the comms transport protocol (e.g. rtp).
func (c *Config) GetCommsProtocol() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsProtocol
}

// GetCommsDebug returns whether comms debug mode is enabled.
func (c *Config) GetCommsDebug() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsDebug
}

// GetCommsLoopback returns whether comms loopback mode is enabled.
func (c *Config) GetCommsLoopback() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsLoopback
}

// GetCommsTrace returns whether comms trace mode is enabled.
func (c *Config) GetCommsTrace() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsTrace
}

// GetCommsControlSource returns the comms control event source backend.
func (c *Config) GetCommsControlSource() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsControlSource
}

// GetCommsPlaybackBuffer returns the playback buffer depth for comms audio.
func (c *Config) GetCommsPlaybackBuffer() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsPlaybackBuffer
}

// GetCommsMicGain returns the microphone gain multiplier applied during transmission.
// Values greater than 1.0 amplify; values between 0 and 1.0 attenuate. Zero or negative
// values fall back to 1.0 (unity gain).
func (c *Config) GetCommsMicGain() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsMicGain
}

// GetCommsNanoPTTEnable returns whether the nanoPTT hardware button is enabled.
func (c *Config) GetCommsNanoPTTEnable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsNanoPTTEnable
}

// GetCommsNanoPTTDevicePath returns the nanoPTT device path glob.
func (c *Config) GetCommsNanoPTTDevicePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsNanoPTTDevicePath
}

// GetCommsNanoPTTDeviceName returns the nanoPTT device name hint.
func (c *Config) GetCommsNanoPTTDeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsNanoPTTDeviceName
}

// GetCommsBluetoothPttEnable returns whether the Bluetooth PTT source is enabled.
func (c *Config) GetCommsBluetoothPttEnable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsBluetoothPttEnable
}

// GetCommsBluetoothPttBluetoothAudioDeviceHint returns a shared matcher for selecting both mic and speaker devices.
func (c *Config) GetCommsBluetoothPttBluetoothAudioDeviceHint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsBluetoothPttBluetoothAudioDeviceHint
}

// GetCommsBluetoothPttBluetoothInputDevice returns the Bluetooth audio input device name or index.
func (c *Config) GetCommsBluetoothPttBluetoothInputDevice() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsBluetoothPttBluetoothInputDevice
}

// GetCommsBluetoothPttBluetoothOutputDevice returns the Bluetooth audio output device name or index.
func (c *Config) GetCommsBluetoothPttBluetoothOutputDevice() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsBluetoothPttBluetoothOutputDevice
}

// GetEnableGNSS returns whether GNSS is enabled.
func (c *Config) GetEnableGNSS() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.EnableGNSS
}

// GetGNSSSendAsNMEA returns whether to send GNSS data as NMEA.
func (c *Config) GetGNSSSendAsNMEA() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.GNSSSendAsNMEA
}

// GetGNSSSendAsCoT returns whether to send GNSS data as CoT.
func (c *Config) GetGNSSSendAsCoT() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.GNSSSendAsCoT
}

// BLOSEnabled returns whether BLOS (Beyond Line of Sight) is enabled.
func (c *Config) BLOSEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.BLOSEnable
}

// GetBLOSStatusWorkerInterval returns the BLOS status worker interval in seconds.
func (c *Config) GetBLOSStatusWorkerInterval() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.BLOSStatusWorkerInterval
}

// GetOpenMANETFrontendURL returns the OpenMANET frontend URL.
func (c *Config) GetOpenMANETFrontendURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.OpenMANETFrontendURL
}

// GetOpenMANETAPIAddress returns the OpenMANET API listen address.
func (c *Config) GetOpenMANETAPIAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.OpenMANETAPIAddress
}

// GetOpenMANETWebsocketPort returns the OpenMANET WebSocket port.
func (c *Config) GetOpenMANETWebsocketPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.OpenMANETWebsocketPort
}
