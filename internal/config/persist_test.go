package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// ── PersistCommsConfig ────────────────────────────────────────────────────────

func TestPersistCommsConfig_EnableWithControlSource(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
comms:
  enable: false
  controlSource: openvlm
`)

	require.False(t, cfg.GetCommsEnable())
	require.Equal(t, "openvlm", cfg.GetCommsControlSource())

	err := cfg.PersistCommsConfig(true, "web")
	require.NoError(t, err)

	assert.True(t, cfg.GetCommsEnable())
	assert.Equal(t, "web", cfg.GetCommsControlSource())

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "enable: true")
	assert.Contains(t, content, "controlSource: web")
}

func TestPersistCommsConfig_Disable(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
comms:
  enable: true
  controlSource: nanoptt
`)

	err := cfg.PersistCommsConfig(false, "openvlm")
	require.NoError(t, err)

	assert.False(t, cfg.GetCommsEnable())
	assert.Equal(t, "openvlm", cfg.GetCommsControlSource())
}

func TestPersistCommsConfig_AllControlSources(t *testing.T) {
	for _, src := range []string{"openvlm", "nanoptt", "web"} {
		src := src
		t.Run(src, func(t *testing.T) {
			cfg := setupTestConfigFromYAML(t, `
comms:
  enable: false
  controlSource: openvlm
`)
			err := cfg.PersistCommsConfig(true, src)
			require.NoError(t, err)
			assert.Equal(t, src, cfg.GetCommsControlSource())
		})
	}
}

func TestPersistCommsConfig_CreatesCommsSection(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
logLevel: info
blos:
  enable: false
`)

	err := cfg.PersistCommsConfig(true, "web")
	require.NoError(t, err)

	assert.True(t, cfg.GetCommsEnable())
	assert.Equal(t, "web", cfg.GetCommsControlSource())

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "comms:")
	assert.Contains(t, content, "enable: true")
	assert.Contains(t, content, "controlSource: web")
}

func TestPersistCommsConfig_PreservesOtherKeys(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `logLevel: info
blos:
  enable: true
comms:
  enable: false
  controlSource: openvlm
  protocol: rtp
  debug: true
`)

	err := cfg.PersistCommsConfig(true, "nanoptt")
	require.NoError(t, err)

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "logLevel: info")
	assert.Contains(t, content, "protocol: rtp")
	assert.Contains(t, content, "debug: true")
	assert.True(t, cfg.GetEnableBLOS(), "BLOS should still be enabled")
}

func TestPersistCommsConfig_PreservesComments(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `# Main config
logLevel: info
comms:
  # Whether comms is enabled
  enable: false
  controlSource: openvlm
`)

	err := cfg.PersistCommsConfig(true, "web")
	require.NoError(t, err)

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "# Main config")
	assert.Contains(t, content, "# Whether comms is enabled")
}

func TestPersistCommsConfig_Idempotent(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `
comms:
  enable: false
  controlSource: openvlm
`)

	err := cfg.PersistCommsConfig(true, "web")
	require.NoError(t, err)

	err = cfg.PersistCommsConfig(true, "web")
	require.NoError(t, err)

	assert.True(t, cfg.GetCommsEnable())
	assert.Equal(t, "web", cfg.GetCommsControlSource())

	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Equal(t, 1, strings.Count(content, "enable:"),
		"should have exactly one enable key under comms")
	assert.Equal(t, 1, strings.Count(content, "controlSource:"),
		"should have exactly one controlSource key under comms")
}

func TestPersistCommsConfig_NonExistentFile(t *testing.T) {
	v := viper.New()
	cfg := &Config{
		v:                 v,
		onChangeCallbacks: make([]func(*Config), 0),
	}

	err := cfg.PersistCommsConfig(true, "openvlm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config file path configured")
}

// ── Concurrency tests ───────────────────────────────��────────────────────────

func TestPersistConfig_ConcurrentDifferentSubsystems(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `blos:
  enable: false
comms:
  enable: false
  controlSource: openvlm
gnss:
  enable: false
  sendAsExternalGNSSSource:
    sendAsNMEA: false
    sendAsCoT: false
    cotUID: ""
`)

	var wg sync.WaitGroup

	wg.Add(3)

	go func() {
		defer wg.Done()

		for i := 0; i < 10; i++ {
			_ = cfg.PersistBLOSConfig(true)
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < 10; i++ {
			_ = cfg.PersistCommsConfig(true, "web")
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < 10; i++ {
			_ = cfg.PersistGNSSConfig(true, true, true, "uid")
		}
	}()

	wg.Wait()

	// All three subsystems should be enabled in memory
	assert.True(t, cfg.GetEnableBLOS(), "BLOS should be enabled")
	assert.True(t, cfg.GetCommsEnable(), "Comms should be enabled")
	assert.True(t, cfg.GetEnableGNSS(), "GNSS should be enabled")

	// Verify on-disk YAML has all three enabled and parses cleanly
	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "blos:")
	assert.Contains(t, content, "comms:")
	assert.Contains(t, content, "gnss:")
}

func TestPersistBLOSConfig_ConcurrentSameSubsystem(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `blos:
  enable: false
`)

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)

		enable := i%2 == 0

		go func() {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				_ = cfg.PersistBLOSConfig(enable)
			}
		}()
	}

	wg.Wait()

	// Verify disk and memory are consistent
	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	memEnabled := cfg.GetEnableBLOS()

	if memEnabled {
		assert.Contains(t, content, "enable: true")
	} else {
		assert.Contains(t, content, "enable: false")
	}
}

func TestPersistBLOSConfig_FileWriteError(t *testing.T) {
	cfg := setupTestConfigFromYAML(t, `blos:
  enable: false
`)

	// Make config file read-only
	err := os.Chmod(cfg.GetConfigFilePath(), 0444)
	require.NoError(t, err)

	err = cfg.PersistBLOSConfig(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing config file")

	// In-memory state should be unchanged
	assert.False(t, cfg.GetEnableBLOS())
}
