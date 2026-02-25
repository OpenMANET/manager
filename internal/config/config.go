package config

import (
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Default configuration values
const (
	DefaultMeshNetInterface            string = "br-ahwlan"
	DefaultGatewayMode                 bool   = false
	DefaultDBFile                      string = "/etc/openmanetd/openmanetd.db"
	DefaultAlfredMode                  string = "primary"
	DefaultAlfredBatInterface          string = "bat0"
	DefaultAlfredSocketPath            string = "/var/run/alfred.sock"
	DefaultAlfredEnable                bool   = true
	DefaultAlfredDataTypeGateway       bool   = true
	DefaultAlfredDataTypeNode          bool   = true
	DefaultAlfredDataTypePosition      bool   = true
	DefaultAlfredDataTypeAddressReserv bool   = true
	DefaultPTTEnable                   bool   = false
	DefaultPTTMcastAddr                string = "224.0.0.1"
	DefaultPTTMcastPort                int    = 5007
	DefaultPTTProtocol                 string = "udp"
	DefaultPTTRtpID                    string = ""
	DefaultPTTPttKey                   string = "any"
	DefaultPTTDebug                    bool   = false
	DefaultPTTLoopback                 bool   = false
	DefaultPTTTrace                    bool   = false
	DefaultPTTPttDevice                string = "/dev/hidraw0/*"
	DefaultPTTPttDeviceName            string = ""
	DefaultPTTControlSource            string = "evdev"
	DefaultPTTAudioDeviceHint          string = ""
	DefaultPTTInputDevice              string = ""
	DefaultPTTOutputDevice             string = ""
	DefaultPTTPlaybackBuffer           int    = 2
	DefaultResetDBOnStart              bool   = false
	DefaultEnableGNSS                  bool   = false
	DefaultGNSSSendAsNMEA              bool   = false
	DefaultGNSSSendAsCoT               bool   = false
	DefaultEnableBLOS                  bool   = false
	DefaultBLOSStatusWorkerInterval    int    = 30 // seconds
)

// Config holds the application configuration values with automatic reloading support.
type Config struct {
	v                           *viper.Viper
	PTTProtocol                 string
	PTTPttDevice                string
	AlfredMode                  string
	AlfredBatInterface          string
	AlfredSocketPath            string
	MeshNetInterface            string
	DBFile                      string
	PTTRtpID                    string
	PTTMcastAddr                string
	PTTPttKey                   string
	PTTOutputDevice             string
	PTTPttDeviceName            string
	PTTControlSource            string
	PTTAudioDeviceHint          string
	PTTInputDevice              string
	onChangeCallbacks           []func(*Config)
	BLOSStatusWorkerInterval    int
	PTTPlaybackBuffer           int
	PTTMcastPort                int
	mu                          sync.RWMutex
	AlfredDataTypeNode          bool
	PTTEnable                   bool
	GatewayMode                 bool
	AlfredDataTypeGateway       bool
	AlfredEnable                bool
	AlfredDataTypePosition      bool
	AlfredDataTypeAddressReserv bool
	BLOSEnable                  bool
	PTTDebug                    bool
	PTTLoopback                 bool
	PTTTrace                    bool
	ResetDBOnStart              bool
	EnableGNSS                  bool
	GNSSSendAsNMEA              bool
	GNSSSendAsCoT               bool
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

// reload reads all configuration values from viper and updates the Config fields.
func (c *Config) reload() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Load mesh network configuration
	if val := c.v.GetString("meshNetInterface"); val != "" {
		c.MeshNetInterface = val
	} else {
		c.MeshNetInterface = DefaultMeshNetInterface
	}

	if c.v.IsSet("gatewayMode") {
		c.GatewayMode = c.v.GetBool("gatewayMode")
	} else {
		c.GatewayMode = DefaultGatewayMode
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

	// Load PTT configuration
	if c.v.IsSet("ptt.enable") {
		c.PTTEnable = c.v.GetBool("ptt.enable")
	} else {
		c.PTTEnable = DefaultPTTEnable
	}

	if val := c.v.GetString("ptt.mcastAddr"); val != "" {
		c.PTTMcastAddr = val
	} else {
		c.PTTMcastAddr = DefaultPTTMcastAddr
	}

	if val := c.v.GetInt("ptt.mcastPort"); val != 0 {
		c.PTTMcastPort = val
	} else {
		c.PTTMcastPort = DefaultPTTMcastPort
	}

	if val := strings.ToLower(c.v.GetString("ptt.protocol")); val != "" {
		c.PTTProtocol = val
	} else {
		c.PTTProtocol = DefaultPTTProtocol
	}

	if val := c.v.GetString("ptt.rtpId"); val != "" {
		c.PTTRtpID = val
	} else {
		c.PTTRtpID = DefaultPTTRtpID
	}

	if val := c.v.GetString("ptt.pttKey"); val != "" {
		c.PTTPttKey = val
	} else {
		c.PTTPttKey = DefaultPTTPttKey
	}

	if c.v.IsSet("ptt.debug") {
		c.PTTDebug = c.v.GetBool("ptt.debug")
	} else {
		c.PTTDebug = DefaultPTTDebug
	}

	if c.v.IsSet("ptt.loopback") {
		c.PTTLoopback = c.v.GetBool("ptt.loopback")
	} else {
		c.PTTLoopback = DefaultPTTLoopback
	}

	if c.v.IsSet("ptt.trace") {
		c.PTTTrace = c.v.GetBool("ptt.trace")
	} else {
		c.PTTTrace = DefaultPTTTrace
	}

	if val := c.v.GetString("ptt.pttDevice"); val != "" {
		c.PTTPttDevice = val
	} else {
		c.PTTPttDevice = DefaultPTTPttDevice
	}

	if val := c.v.GetString("ptt.pttDeviceName"); val != "" {
		c.PTTPttDeviceName = val
	} else {
		c.PTTPttDeviceName = DefaultPTTPttDeviceName
	}

	if val := strings.ToLower(c.v.GetString("ptt.controlSource")); val != "" {
		c.PTTControlSource = val
	} else {
		c.PTTControlSource = DefaultPTTControlSource
	}

	if val := c.v.GetString("ptt.audioDeviceHint"); val != "" {
		c.PTTAudioDeviceHint = val
	} else {
		c.PTTAudioDeviceHint = DefaultPTTAudioDeviceHint
	}

	if val := c.v.GetString("ptt.inputDevice"); val != "" {
		c.PTTInputDevice = val
	} else {
		c.PTTInputDevice = DefaultPTTInputDevice
	}

	if val := c.v.GetString("ptt.outputDevice"); val != "" {
		c.PTTOutputDevice = val
	} else {
		c.PTTOutputDevice = DefaultPTTOutputDevice
	}

	if val := c.v.GetInt("ptt.playbackBuffer"); val > 0 {
		c.PTTPlaybackBuffer = val
	} else {
		c.PTTPlaybackBuffer = DefaultPTTPlaybackBuffer
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

// GetGatewayMode returns whether gateway mode is enabled.
func (c *Config) GetGatewayMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.GatewayMode
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

// GetPTTEnable returns whether PTT (Push-to-Talk) is enabled.
func (c *Config) GetPTTEnable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTEnable
}

// GetPTTMcastAddr returns the PTT multicast address.
func (c *Config) GetPTTMcastAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTMcastAddr
}

// GetPTTMcastPort returns the PTT multicast port.
func (c *Config) GetPTTMcastPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTMcastPort
}

// GetPTTProtocol returns the PTT protocol (udp or rtp).
func (c *Config) GetPTTProtocol() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTProtocol
}

// GetPTTRtpID returns the RTP identifier used to derive SSRC.
func (c *Config) GetPTTRtpID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTRtpID
}

// GetPTTPttKey returns the PTT key configuration.
func (c *Config) GetPTTPttKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTPttKey
}

// GetPTTDebug returns whether PTT debug mode is enabled.
func (c *Config) GetPTTDebug() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTDebug
}

// GetPTTLoopback returns whether PTT loopback mode is enabled.
func (c *Config) GetPTTLoopback() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTLoopback
}

// GetPTTTrace returns whether PTT trace mode is enabled.
func (c *Config) GetPTTTrace() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTTrace
}

// GetPTTPttDevice returns the PTT device path.
func (c *Config) GetPTTPttDevice() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTPttDevice
}

// GetPTTPttDeviceName returns the PTT device name.
func (c *Config) GetPTTPttDeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTPttDeviceName
}

// GetPTTControlSource returns the PTT control event source backend.
func (c *Config) GetPTTControlSource() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTControlSource
}

// GetPTTAudioDeviceHint returns a shared matcher for selecting both mic and speaker devices.
func (c *Config) GetPTTAudioDeviceHint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTAudioDeviceHint
}

// GetPTTInputDevice returns the audio input device name or index.
func (c *Config) GetPTTInputDevice() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTInputDevice
}

// GetPTTOutputDevice returns the audio output device name or index.
func (c *Config) GetPTTOutputDevice() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTOutputDevice
}

// GetPTTPlaybackBuffer returns the playback buffer depth for PTT audio.
func (c *Config) GetPTTPlaybackBuffer() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.PTTPlaybackBuffer
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
