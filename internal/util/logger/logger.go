package logger

import (
	"context"
	stdlog "log"
	"os"
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
)

// InitLogging initializes the logging configuration
func InitLogging(ctx context.Context) zerolog.Logger {
	zerolog.TimestampFieldName = timestampFieldName
	zerolog.MessageFieldName = MessageFieldName
	zerolog.ErrorFieldName = errorFieldName

	// UNIX Time is faster and smaller than most timestamps
	zerolog.TimeFieldFormat = time.RFC3339

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}

	zlog := zerolog.New(output)

	zlog = zlog.With().Timestamp().
		Ctx(ctx).
		Stack().
		Logger()

	// Set Global Log Level From Environment Configuration
	setLogLevel(viper.GetString("logLevel"))

	// Set our logger as the writer for standard library log
	stdlog.SetFlags(0)
	stdlog.SetOutput(zlog)

	return zlog
}

// getLogger returns a logger with the given component name
func getLogger(component string) zerolog.Logger {

	zerolog.TimestampFieldName = timestampFieldName
	zerolog.MessageFieldName = MessageFieldName
	zerolog.ErrorFieldName = errorFieldName

	// UNIX Time is faster and smaller than most timestamps
	zerolog.TimeFieldFormat = time.RFC3339

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	/* output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		FormatLevel: func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("[%s]", i))
		},
		FormatMessage: func(i interface{}) string {
			return fmt.Sprintf("*%s*", i)
		},
		FormatFieldName: func(i interface{}) string {
			return fmt.Sprintf("%s:", i)
		},
		FormatFieldValue: func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("%s", i))
		},
	} */

	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}

	zlog := zerolog.New(output)

	zlog = zlog.With().Timestamp().
		Str(LogComponentFieldName, component).
		Stack().
		Logger()

	// Set Global Log Level From Environment Configuration
	setLogLevel(viper.GetString("logLevel"))

	// Set our logger as the writer for standard library log
	stdlog.SetFlags(0)
	stdlog.SetOutput(zlog)

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
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
