package logger

import (
	"context"
	"fmt"
	"log"
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

	// Use RFC3339 for human-readable timestamps
	zerolog.TimeFieldFormat = time.RFC3339

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	output := zerolog.ConsoleWriter{
		Out:           os.Stdout,
		TimeFormat:    time.RFC3339,
		PartsOrder:    []string{zerolog.LevelFieldName, LogComponentFieldName, MessageFieldName},
		FieldsExclude: []string{zerolog.TimestampFieldName, LogComponentFieldName},
	}

	zlog := zerolog.New(output)

	zlog = zlog.With().
		Ctx(ctx).
		Stack().
		Logger()

	// Set Global Log Level From Environment Configuration
	setLogLevel(viper.GetString("logLevel"))

	// Set our logger as the writer for standard library log
	//log.SetFlags(0)
	//log.SetOutput(zlog)

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

	output := zerolog.ConsoleWriter{
		Out:           os.Stdout,
		TimeFormat:    time.RFC3339,
		PartsOrder:    []string{zerolog.LevelFieldName, LogComponentFieldName, MessageFieldName},
		FieldsExclude: []string{zerolog.TimestampFieldName, LogComponentFieldName},
	}

	zlog := zerolog.New(output)

	zlog = zlog.With().
		Str(LogComponentFieldName, component).
		Stack().
		Logger()

	// Set Global Log Level From Environment Configuration
	setLogLevel(viper.GetString("logLevel"))

	// Set our logger as the writer for standard library log
	//log.SetFlags(0)
	//log.SetOutput(zlog)

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

type stdLogger struct {
	log zerolog.Logger
}

func (s *stdLogger) Fatal(v ...interface{}) {
	s.log.Fatal().Msg(fmt.Sprint(v...))
	os.Exit(1)
}

func (s *stdLogger) Fatalf(format string, v ...interface{}) {
	s.log.Fatal().Msg(fmt.Sprintf(format, v...))
	os.Exit(1)
}

func (s *stdLogger) Print(v ...interface{}) {
	s.log.Info().Msg(fmt.Sprint(v...))
}

func (s *stdLogger) Println(v ...interface{}) {
	s.log.Info().Msg(fmt.Sprintln(v...))
}

func (s *stdLogger) Printf(format string, v ...interface{}) {
	s.log.Info().Msg(fmt.Sprintf(format, v...))
}
