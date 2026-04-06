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
	DefaultCommsMicGain                              float32 = 8.0
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
	DefaultGNSSCoTUID                                string  = ""
	DefaultEnableBLOS                                bool    = false
	DefaultBLOSStatusWorkerInterval                  int     = 30 // seconds
	DefaultOpenMANETFrontendHostPort                 string  = "0.0.0.0:8080"
	DefaultOpenMANETFrontendTLSHostPort              string  = "0.0.0.0:8081"
	DefaultOpenMANETFrontendTLSCertFile              string  = ""
	DefaultOpenMANETFrontendTLSKeyFile               string  = ""
	DefaultOpenMANETWebsocketPort                    int     = 0
	DefaultOpenMANETAPIAddress                       string  = "0.0.0.0:8087"
	DefaultOpenMANETCommsAPIAddress                  string  = "http://127.0.0.1:8087"
	DefaultRuntimeMemLimit                           string  = "64MiB"
	DefaultRuntimeGoGC                               int     = 50
	DefaultDebugPprof                                bool    = false
	DefaultDebugPprofAddress                         string  = "127.0.0.1:6060"
	DefaultCommsEncoderComplexity                    int     = 5
	DefaultAuthEnable                                bool    = false
	DefaultAuthSessionMaxAgeSecs                     int     = 86400 // 24 hours
	DefaultAuthSessionMaxSize                        int     = 16
	DefaultAuthPAMService                            string  = "login"
)

// Config holds the application configuration values with automatic reloading support.
type Config struct {
	v                                         *viper.Viper
	OpenMANETFrontendTLSHostPort              string
	CommsNanoPTTDeviceName                    string
	AlfredMode                                string
	AlfredBatInterface                        string
	OpenMANETFrontendTLSCertFile              string
	MeshNetInterface                          string
	CommsNanoPTTDevicePath                    string
	CommsBluetoothPttBluetoothOutputDevice    string
	DBFile                                    string
	CommsControlSource                        string
	CommsProtocol                             string
	CommsBluetoothPttBluetoothInputDevice     string
	CommsBluetoothPttBluetoothAudioDeviceHint string
	OpenMANETFrontendHostPort                 string
	AlfredSocketPath                          string
	OpenMANETFrontendTLSKeyFile               string
	OpenMANETAPIAddress                       string
	OpenMANETCommsAPIAddress                  string
	RuntimeMemLimit                           string
	DebugPprofAddress                         string
	AuthPAMService                            string
	GNSSCoTUID                                string
	onChangeCallbacks                         []func(*Config)
	BLOSStatusWorkerInterval                  int
	OpenMANETWebsocketPort                    int
	CommsEncoderComplexity                    int
	RuntimeGoGC                               int
	AuthSessionMaxAgeSecs                     int
	AuthSessionMaxSize                        int
	mu                                        sync.RWMutex
	persistMu                                 sync.Mutex // serializes Persist*Config file I/O
	CommsMicGain                              float32
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
	DebugPprof                                bool
	AlfredDataTypePosition                    bool
	AlfredEnable                              bool
	AlfredDataTypeGateway                     bool
	CommsLoopback                             bool
	BLOSEnable                                bool
	AuthEnable                                bool
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

	if val := c.v.GetString("gnss.sendAsExternalGNSSSource.cotUID"); val != "" {
		c.GNSSCoTUID = val
	} else {
		c.GNSSCoTUID = DefaultGNSSCoTUID
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

	if val := c.v.GetString("openmanetFrontendHostPort"); val != "" {
		c.OpenMANETFrontendHostPort = val
	} else {
		c.OpenMANETFrontendHostPort = DefaultOpenMANETFrontendHostPort
	}

	if val := c.v.GetString("frontend.tlsHostPort"); val != "" {
		c.OpenMANETFrontendTLSHostPort = val
	} else {
		c.OpenMANETFrontendTLSHostPort = DefaultOpenMANETFrontendTLSHostPort
	}

	if val := c.v.GetString("frontend.tlsCertFile"); val != "" {
		c.OpenMANETFrontendTLSCertFile = val
	} else {
		c.OpenMANETFrontendTLSCertFile = DefaultOpenMANETFrontendTLSCertFile
	}

	if val := c.v.GetString("frontend.tlsKeyFile"); val != "" {
		c.OpenMANETFrontendTLSKeyFile = val
	} else {
		c.OpenMANETFrontendTLSKeyFile = DefaultOpenMANETFrontendTLSKeyFile
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

	if val := c.v.GetString("openmanetCommsAPIAddress"); val != "" {
		c.OpenMANETCommsAPIAddress = val
	} else {
		c.OpenMANETCommsAPIAddress = DefaultOpenMANETCommsAPIAddress
	}

	// Load runtime configuration
	if val := c.v.GetString("runtime.memlimit"); val != "" {
		c.RuntimeMemLimit = val
	} else {
		c.RuntimeMemLimit = DefaultRuntimeMemLimit
	}

	if c.v.IsSet("runtime.gogc") {
		c.RuntimeGoGC = c.v.GetInt("runtime.gogc")
	} else {
		c.RuntimeGoGC = DefaultRuntimeGoGC
	}

	// Load debug configuration
	if c.v.IsSet("debug.pprof") {
		c.DebugPprof = c.v.GetBool("debug.pprof")
	} else {
		c.DebugPprof = DefaultDebugPprof
	}

	if val := c.v.GetString("debug.pprofAddress"); val != "" {
		c.DebugPprofAddress = val
	} else {
		c.DebugPprofAddress = DefaultDebugPprofAddress
	}

	// Load comms encoder complexity
	if c.v.IsSet("comms.encoderComplexity") {
		c.CommsEncoderComplexity = c.v.GetInt("comms.encoderComplexity")
	} else {
		c.CommsEncoderComplexity = DefaultCommsEncoderComplexity
	}

	// Load auth configuration
	if c.v.IsSet("auth.enable") {
		c.AuthEnable = c.v.GetBool("auth.enable")
	} else {
		c.AuthEnable = DefaultAuthEnable
	}

	if val := c.v.GetInt("auth.sessionMaxAge"); val > 0 {
		c.AuthSessionMaxAgeSecs = val
	} else {
		c.AuthSessionMaxAgeSecs = DefaultAuthSessionMaxAgeSecs
	}

	if val := c.v.GetInt("auth.sessionMaxSize"); val > 0 {
		c.AuthSessionMaxSize = val
	} else {
		c.AuthSessionMaxSize = DefaultAuthSessionMaxSize
	}

	if val := c.v.GetString("auth.pamService"); val != "" {
		c.AuthPAMService = val
	} else {
		c.AuthPAMService = DefaultAuthPAMService
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

// GetGNSSCoTUID returns the CoT UID for GNSS messages.
func (c *Config) GetGNSSCoTUID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.GNSSCoTUID
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

// GetOpenMANETFrontendHostPort returns the OpenMANET frontend host and port.
func (c *Config) GetOpenMANETFrontendHostPort() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.OpenMANETFrontendHostPort
}

// GetOpenMANETFrontendTLSHostPort returns the TLS listen address for the frontend server.
func (c *Config) GetOpenMANETFrontendTLSHostPort() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.OpenMANETFrontendTLSHostPort
}

// GetOpenMANETFrontendTLSCertFile returns the path to the TLS certificate file.
func (c *Config) GetOpenMANETFrontendTLSCertFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.OpenMANETFrontendTLSCertFile
}

// GetOpenMANETFrontendTLSKeyFile returns the path to the TLS private key file.
func (c *Config) GetOpenMANETFrontendTLSKeyFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.OpenMANETFrontendTLSKeyFile
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

// GetOpenMANETCommsAPIAddress returns the OpenMANET comms API address.
func (c *Config) GetOpenMANETCommsAPIAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.OpenMANETCommsAPIAddress
}

// SetOpenMANETAPIAddress overrides the OpenMANET API address.
// This is used by the frontend-only dev mode to point at a remote instance.
func (c *Config) SetOpenMANETAPIAddress(addr string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.OpenMANETAPIAddress = addr
}

// SetOpenMANETWebsocketPort overrides the OpenMANET WebSocket port.
// This is used by the frontend-only dev mode to bind the local frontend server.
func (c *Config) SetOpenMANETWebsocketPort(port int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.OpenMANETWebsocketPort = port
}

// SetOpenMANETFrontendHostPort overrides the OpenMANET frontend host and port.
// This is used by the frontend-only dev mode to bind the local frontend server.
func (c *Config) SetOpenMANETFrontendHostPort(hostPort string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.OpenMANETFrontendHostPort = hostPort
}

// SetOpenMANETCommsAPIAddress overrides the OpenMANET comms API address.
// This is used by the frontend-only dev mode to point at a remote instance.
func (c *Config) SetOpenMANETCommsAPIAddress(addr string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.OpenMANETCommsAPIAddress = addr
}

// GetRuntimeMemLimit returns the runtime memory limit string (e.g. "64MiB").
func (c *Config) GetRuntimeMemLimit() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.RuntimeMemLimit
}

// GetRuntimeGoGC returns the GOGC percentage value.
func (c *Config) GetRuntimeGoGC() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.RuntimeGoGC
}

// GetDebugPprof returns whether the pprof debug endpoint is enabled.
func (c *Config) GetDebugPprof() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.DebugPprof
}

// GetDebugPprofAddress returns the pprof listen address.
func (c *Config) GetDebugPprofAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.DebugPprofAddress
}

// GetCommsEncoderComplexity returns the Opus encoder complexity (0-10).
func (c *Config) GetCommsEncoderComplexity() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.CommsEncoderComplexity
}

// GetAuthEnable returns whether HTTP authentication is enabled.
func (c *Config) GetAuthEnable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AuthEnable
}

// GetAuthSessionMaxAgeSecs returns the session lifetime in seconds.
func (c *Config) GetAuthSessionMaxAgeSecs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AuthSessionMaxAgeSecs
}

// GetAuthSessionMaxSize returns the maximum number of concurrent sessions.
func (c *Config) GetAuthSessionMaxSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AuthSessionMaxSize
}

// GetAuthPAMService returns the PAM service name used for authentication.
func (c *Config) GetAuthPAMService() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.AuthPAMService
}
