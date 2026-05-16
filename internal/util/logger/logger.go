package logger

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
	"github.com/spf13/viper"
)

const (
	// timestampFieldName is the key for the timestamp field in the log
	timestampFieldName string = "time"

	// messageFieldName is the key for the message field in the log
	MessageFieldName string = "message"

	// errorFieldName is the key for the error field in the log
	errorFieldName string = "error"

	// ComponentFieldName is the key for the component field in the log
	LogComponentFieldName string = "component"

	// Accepted log-level strings. Mirror zerolog's level names so config
	// values like "info" / "debug" map cleanly.
	logLevelDebug = "debug"
	logLevelInfo  = "info"
	logLevelWarn  = "warn"
	logLevelFatal = "fatal"
	logLevelPanic = "panic"
)

// Shared console writer instance to reduce memory allocations
var sharedConsoleWriter = zerolog.ConsoleWriter{ //nolint:gochecknoglobals
	Out:           os.Stdout,
	TimeFormat:    time.RFC3339,
	PartsOrder:    []string{zerolog.LevelFieldName, LogComponentFieldName, MessageFieldName},
	FieldsExclude: []string{zerolog.TimestampFieldName, LogComponentFieldName},
}

// initOnce guards one-time mutation of the zerolog package globals so
// concurrent callers of GetLogger don't race the writer goroutine that
// reads those same globals while formatting fields.
var initOnce sync.Once //nolint:gochecknoglobals

// initZerologGlobals configures the zerolog package-level globals exactly
// once. These fields are read concurrently by the shared ConsoleWriter, so
// they must not be re-assigned on every GetLogger call.
func initZerologGlobals() {
	initOnce.Do(func() {
		zerolog.TimestampFieldName = timestampFieldName
		zerolog.MessageFieldName = MessageFieldName
		zerolog.ErrorFieldName = errorFieldName
		zerolog.TimeFieldFormat = time.RFC3339
		zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	})
}

// InitLogging initializes the logging configuration
func InitLogging(ctx context.Context) zerolog.Logger {
	initZerologGlobals()

	zlog := zerolog.New(sharedConsoleWriter)

	zlog = zlog.With().
		Ctx(ctx).
		Stack().
		Logger()

	// Set Global Log Level From Environment Configuration
	setLogLevel(viper.GetString("logLevel"))

	return zlog
}

// getLogger returns a logger with the given component name
func getLogger(component string) zerolog.Logger {
	initZerologGlobals()

	zlog := zerolog.New(sharedConsoleWriter)

	zlog = zlog.With().
		Str(LogComponentFieldName, component).
		Stack().
		Logger()

	// Set Global Log Level From Environment Configuration
	setLogLevel(viper.GetString("logLevel"))

	return zlog
}

// GetLogger returns a logger with the given component name and additional standard fields attached
func GetLogger(component string) zerolog.Logger {
	return getLogger(component)
}

// GetLoggerFromContext returns a logger from context for the given component name and additional standard fields attached
func GetLoggerFromContext(ctx context.Context, component string) zerolog.Logger {
	var (
		log = zerolog.Ctx(ctx)
	)

	return log.With().
		Ctx(ctx).
		Str(LogComponentFieldName, component).
		Stack().Logger()
}

// setLogLevel sets the global log level based on the environment configuration
func setLogLevel(env string) {
	switch env {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case logLevelDebug:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case logLevelInfo:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case logLevelWarn:
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case errorFieldName:
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case logLevelFatal:
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case logLevelPanic:
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

// Wraps a zerolog.Logger so its interoperable with Go's standard "log" package

var _ StdLogger = &log.Logger{}

type StdLogger interface {
	Fatal(v ...interface{})
	Fatalf(format string, v ...interface{})
	Print(v ...interface{})
	Println(v ...interface{})
	Printf(format string, v ...interface{})
}

func StandardLogger(zlog zerolog.Logger) *log.Logger {
	return log.New(&zerologWriter{zlog}, "", 0)
}

type zerologWriter struct {
	log zerolog.Logger
}

func (w *zerologWriter) Write(p []byte) (n int, err error) {
	w.log.Info().Msg(string(p))

	return len(p), nil
}
