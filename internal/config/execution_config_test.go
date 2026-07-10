package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newConfigFromYAML(t *testing.T, yamlContent string) *Config {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	v := viper.New()
	v.SetConfigFile(cfgPath)
	require.NoError(t, v.ReadInConfig())

	return NewWithoutWatch(v)
}

func TestGetRuntimeGOMAXPROCS_DefaultWhenUnset(t *testing.T) {
	cfg := newConfigFromYAML(t, "runtime:\n  gogc: 50\n")
	assert.Equal(t, DefaultRuntimeGOMAXPROCS, cfg.GetRuntimeGOMAXPROCS())
	assert.Equal(t, 0, cfg.GetRuntimeGOMAXPROCS())
}

func TestGetRuntimeGOMAXPROCS_ReadsConfiguredValue(t *testing.T) {
	cfg := newConfigFromYAML(t, "runtime:\n  gomaxprocs: 2\n")
	assert.Equal(t, 2, cfg.GetRuntimeGOMAXPROCS())
}
