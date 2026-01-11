package logger

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

func TestInitLogging(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		wantMsg  string
	}{
		{
			name:     "initializes logger with debug level",
			logLevel: "debug",
			wantMsg:  "test message",
		},
		{
			name:     "initializes logger with info level",
			logLevel: "info",
			wantMsg:  "test message",
		},
		{
			name:     "initializes logger with warn level",
			logLevel: "warn",
			wantMsg:  "test message",
		},
		{
			name:     "initializes logger with error level",
			logLevel: "error",
			wantMsg:  "test message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper for each test
			v := viper.New()
			v.Set("logLevel", tt.logLevel)
			viper.Reset()
			viper.Set("logLevel", tt.logLevel)

			ctx := context.Background()
			log := InitLogging(ctx)

			// Verify the logger is not nil
			if log.GetLevel() == zerolog.Disabled {
				t.Error("InitLogging() returned a disabled logger")
			}

			// Verify the log level was set correctly
			expectedLevel := zerolog.InfoLevel
			switch tt.logLevel {
			case "debug":
				expectedLevel = zerolog.DebugLevel
			case "info":
				expectedLevel = zerolog.InfoLevel
			case "warn":
				expectedLevel = zerolog.WarnLevel
			case "error":
				expectedLevel = zerolog.ErrorLevel
			case "fatal":
				expectedLevel = zerolog.FatalLevel
			case "panic":
				expectedLevel = zerolog.PanicLevel
			}

			if zerolog.GlobalLevel() != expectedLevel {
				t.Errorf("Expected global log level %v, got %v", expectedLevel, zerolog.GlobalLevel())
			}
		})
	}
}

func TestGetLogger(t *testing.T) {
	tests := []struct {
		name      string
		component string
	}{
		{
			name:      "creates logger with component name",
			component: "test-component",
		},
		{
			name:      "creates logger with different component name",
			component: "another-component",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper
			viper.Reset()
			viper.Set("logLevel", "info")

			log := GetLogger(tt.component)

			// Verify the logger is not nil and functional
			if log.GetLevel() == zerolog.Disabled {
				t.Error("GetLogger() returned a disabled logger")
			}
		})
	}
}

func TestGetLoggerFromContext(t *testing.T) {
	tests := []struct {
		name      string
		component string
	}{
		{
			name:      "creates logger from context with component",
			component: "ctx-component",
		},
		{
			name:      "creates logger from context with different component",
			component: "another-ctx-component",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a context with a logger
			logger := zerolog.New(bytes.NewBuffer(nil)).With().Logger()
			ctx := logger.WithContext(context.Background())

			log := GetLoggerFromContext(ctx, tt.component)

			// Verify the logger is not nil and functional
			if log.GetLevel() == zerolog.Disabled {
				t.Error("GetLoggerFromContext() returned a disabled logger")
			}
		})
	}
}

func TestSetLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		want     zerolog.Level
	}{
		{
			name:     "sets debug level",
			logLevel: "debug",
			want:     zerolog.DebugLevel,
		},
		{
			name:     "sets info level",
			logLevel: "info",
			want:     zerolog.InfoLevel,
		},
		{
			name:     "sets warn level",
			logLevel: "warn",
			want:     zerolog.WarnLevel,
		},
		{
			name:     "sets error level",
			logLevel: "error",
			want:     zerolog.ErrorLevel,
		},
		{
			name:     "sets fatal level",
			logLevel: "fatal",
			want:     zerolog.FatalLevel,
		},
		{
			name:     "sets panic level",
			logLevel: "panic",
			want:     zerolog.PanicLevel,
		},
		{
			name:     "defaults to info level for unknown value",
			logLevel: "unknown",
			want:     zerolog.InfoLevel,
		},
		{
			name:     "defaults to info level for empty string",
			logLevel: "",
			want:     zerolog.InfoLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setLogLevel(tt.logLevel)

			if got := zerolog.GlobalLevel(); got != tt.want {
				t.Errorf("setLogLevel(%q) set global level to %v, want %v", tt.logLevel, got, tt.want)
			}
		})
	}
}

func TestLoggerConstants(t *testing.T) {
	// Verify that constants are set correctly
	if timestampFieldName != "time" {
		t.Errorf("timestampFieldName = %q, want %q", timestampFieldName, "time")
	}
	if MessageFieldName != "message" {
		t.Errorf("MessageFieldName = %q, want %q", MessageFieldName, "message")
	}
	if errorFieldName != "error" {
		t.Errorf("errorFieldName = %q, want %q", errorFieldName, "error")
	}
	if LogComponentFieldName != "component" {
		t.Errorf("LogComponentFieldName = %q, want %q", LogComponentFieldName, "component")
	}
}

func TestLoggerFieldNames(t *testing.T) {
	// Test that zerolog uses the correct field names after initialization
	viper.Reset()
	viper.Set("logLevel", "info")

	ctx := context.Background()
	_ = InitLogging(ctx)

	if zerolog.TimestampFieldName != timestampFieldName {
		t.Errorf("zerolog.TimestampFieldName = %q, want %q", zerolog.TimestampFieldName, timestampFieldName)
	}
	if zerolog.MessageFieldName != MessageFieldName {
		t.Errorf("zerolog.MessageFieldName = %q, want %q", zerolog.MessageFieldName, MessageFieldName)
	}
	if zerolog.ErrorFieldName != errorFieldName {
		t.Errorf("zerolog.ErrorFieldName = %q, want %q", zerolog.ErrorFieldName, errorFieldName)
	}
}

func TestGetLoggerMultipleCalls(t *testing.T) {
	// Test that calling GetLogger multiple times with different components works correctly
	viper.Reset()
	viper.Set("logLevel", "info")

	log1 := GetLogger("component1")
	log2 := GetLogger("component2")

	// Both loggers should be functional
	if log1.GetLevel() == zerolog.Disabled {
		t.Error("First logger is disabled")
	}
	if log2.GetLevel() == zerolog.Disabled {
		t.Error("Second logger is disabled")
	}
}
