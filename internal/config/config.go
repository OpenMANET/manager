package config

import (
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Default configuration values
const (
	DefaultMeshNetInterface            = "br-ahwlan"
	DefaultGatewayMode                 = false
	DefaultAlfredMode                  = "primary"
	DefaultAlfredBatInterface          = "bat0"
	DefaultAlfredSocketPath            = "/var/run/alfred.sock"
	DefaultAlfredDataTypeGateway       = true
	DefaultAlfredDataTypeNode          = true
	DefaultAlfredDataTypePosition      = true
	DefaultAlfredDataTypeAddressReserv = true
	DefaultPTTEnable                   = false
	DefaultPTTMcastAddr                = "224.0.0.1"
	DefaultPTTMcastPort                = 5007
	DefaultPTTPttKey                   = "any"
	DefaultPTTDebug                    = false
	DefaultPTTLoopback                 = false
	DefaultPTTPttDevice                = "/dev/hidraw0/*"
	DefaultPTTPttDeviceName            = ""
)

// Config holds the application configuration values with automatic reloading support.
type Config struct {
	mu                          sync.RWMutex
	v                           *viper.Viper
	MeshNetInterface            string
	GatewayMode                 bool
	AlfredMode                  string
	AlfredBatInterface          string
	AlfredSocketPath            string
	AlfredDataTypeGateway       bool
	AlfredDataTypeNode          bool
	AlfredDataTypePosition      bool
	AlfredDataTypeAddressReserv bool
	PTTEnable                   bool
	PTTMcastAddr                string
	PTTMcastPort                int
	PTTPttKey                   string
	PTTDebug                    bool
	PTTLoopback                 bool
	PTTPttDevice                string
	PTTPttDeviceName            string
	onChangeCallbacks           []func(*Config)
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
