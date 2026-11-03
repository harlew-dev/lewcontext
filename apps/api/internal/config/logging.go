package config

import (
	"errors"
	"log/slog"
	"os"
)

// Logging is how the server logs (docs/api/observability.md). Every command
// reads it first, with LoadLogging, so even a command whose other settings
// are refused logs in the chosen format; Load reads it again into Config.
// In a Config built by hand (the tests), the zero value logs every request.
type Logging struct {
	// LogFormat (LOG_FORMAT) is json (observe.FormatJSON), the default in
	// production, or text (observe.FormatText), the default elsewhere.
	LogFormat string
	// LogLevel (LOG_LEVEL) is the least severe level logged: debug, info
	// (the default), warn or error.
	LogLevel slog.Level
	// AccessLog (ACCESS_LOG) is which requests get an access line:
	// AccessLogAll (the default), AccessLogErrors or AccessLogOff.
	AccessLog string
}

// ACCESS_LOG's values.
const (
	// AccessLogAll logs a line for every request.
	AccessLogAll = "all"
	// AccessLogErrors logs only 4xx and 5xx answers and requests over a
	// second, for a log store that charges by size.
	AccessLogErrors = "errors"
	// AccessLogOff logs no request lines.
	AccessLogOff = "off"
)

// logLevels are LOG_LEVEL's values.
var logLevels = map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}

// LoadLogging reads LOG_FORMAT, LOG_LEVEL and ACCESS_LOG, refusing values
// it doesn't know.
func LoadLogging() (Logging, error) {
	var l Logging
	format := "text"
	if os.Getenv("APP_ENV") == "production" {
		format = "json"
	}
	l.LogFormat = env("LOG_FORMAT", format)
	if l.LogFormat != "json" && l.LogFormat != "text" {
		return l, errors.New("LOG_FORMAT must be json or text")
	}
	level, ok := logLevels[env("LOG_LEVEL", "info")]
	if !ok {
		return l, errors.New("LOG_LEVEL must be debug, info, warn or error")
	}
	l.LogLevel = level
	l.AccessLog = env("ACCESS_LOG", AccessLogAll)
	if l.AccessLog != AccessLogAll && l.AccessLog != AccessLogErrors && l.AccessLog != AccessLogOff {
		return l, errors.New("ACCESS_LOG must be all, errors or off")
	}
	return l, nil
}
