package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestConfigFromYAML creates a Config backed by a temp YAML file.
// It does NOT start the file watcher to avoid race conditions in tests.
func setupTestConfigFromYAML(t *testing.T, yamlContent string) *Config {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)

	err = v.ReadInConfig()
	require.NoError(t, err)

	return NewWithoutWatch(v)
}

func TestPersistBLOSConfig_EnableInExistingFile(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
blos:
  enable: false
`)

	require.False(t, cfg.GetEnableBLOS())

	err := cfg.PersistBLOSConfig(true)
	require.NoError(t, err)

	// Verify in-memory state updated
	assert.True(t, cfg.GetEnableBLOS())

	// Verify file updated
	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)
	assert.Contains(t, string(data), "enable: true")
}

func TestPersistBLOSConfig_DisableInExistingFile(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
blos:
  enable: true
`)

	require.True(t, cfg.GetEnableBLOS())

	err := cfg.PersistBLOSConfig(false)
	require.NoError(t, err)

	assert.False(t, cfg.GetEnableBLOS())

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)
	assert.Contains(t, string(data), "enable: false")
}

func TestPersistBLOSConfig_CreatesBLOSSection(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
logLevel: info
comms:
  enable: false
`)

	err := cfg.PersistBLOSConfig(true)
	require.NoError(t, err)

	assert.True(t, cfg.GetEnableBLOS())

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "blos")
	assert.Contains(t, content, "enable: true")
}

func TestPersistBLOSConfig_PreservesOtherKeys(t *testing.T) {
	yamlContent := `logLevel: info
gnss:
  enable: true
  sendAsExternalGNSSSource:
    sendAsNMEA: true
    sendAsCoT: true
alfred:
  dataTypes:
    gateway: true
    node: true
    addressReservation: true
blos:
  enable: false
comms:
  enable: false
  protocol: rtp
  debug: true
`
	cfg := setupTestConfigFromYAML(t, yamlContent)

	err := cfg.PersistBLOSConfig(true)
	require.NoError(t, err)

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)

	// BLOS updated
	assert.True(t, cfg.GetEnableBLOS())

	// Other keys preserved
	assert.Contains(t, content, "logLevel: info")
	assert.Contains(t, content, "protocol: rtp")
	assert.Contains(t, content, "sendAsNMEA: true")
	assert.Contains(t, content, "gateway: true")

	// Comms should still be disabled
	assert.False(t, cfg.GetCommsEnable())
}

func TestPersistBLOSConfig_PreservesComments(t *testing.T) {
	yamlContent := `# Main config
logLevel: info
blos:
  # Whether BLOS is enabled
  enable: false
comms:
  enable: false
`
	cfg := setupTestConfigFromYAML(t, yamlContent)

	err := cfg.PersistBLOSConfig(true)
	require.NoError(t, err)

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)

	assert.Contains(t, content, "# Main config")
	assert.Contains(t, content, "# Whether BLOS is enabled")
}

func TestPersistBLOSConfig_UpdatesInMemoryState(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
blos:
  enable: false
`)

	assert.False(t, cfg.GetEnableBLOS())

	err := cfg.PersistBLOSConfig(true)
	require.NoError(t, err)

	// In-memory state should be updated immediately
	assert.True(t, cfg.GetEnableBLOS())
	assert.True(t, cfg.BLOSEnabled())

	// Toggle back
	err = cfg.PersistBLOSConfig(false)
	require.NoError(t, err)

	assert.False(t, cfg.GetEnableBLOS())
	assert.False(t, cfg.BLOSEnabled())
}

func TestPersistBLOSConfig_NonExistentFile(t *testing.T) {
	v := viper.New()
	// Don't set a config file, so ConfigFileUsed() returns ""
	cfg := &Config{
		v:                 v,
		onChangeCallbacks: make([]func(*Config), 0),
	}

	err := cfg.PersistBLOSConfig(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config file path configured")
}

func TestPersistBLOSConfig_Idempotent(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
blos:
  enable: false
`)

	// Enable twice
	err := cfg.PersistBLOSConfig(true)
	require.NoError(t, err)

	err = cfg.PersistBLOSConfig(true)
	require.NoError(t, err)

	assert.True(t, cfg.GetEnableBLOS())

	// Verify file doesn't have duplicate keys
	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Equal(t, 1, strings.Count(content, "enable:"),
		"should have exactly one enable key under blos")
}
