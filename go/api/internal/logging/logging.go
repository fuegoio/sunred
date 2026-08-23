// Package logging configures the process-wide log format using log/slog.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

type Format int

const (
	FormatPretty Format = iota
	FormatJSON
)

// Init configures the default slog logger with the chosen format and destination.
// level controls the minimum verbosity ("debug", "info", "warn", "error");
// empty defaults to info.
func Init(format, level string, out io.Writer) (*slog.Logger, error) {
	if out == nil {
		out = os.Stderr
	}
	lvl := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return nil, fmt.Errorf("unknown log level %q (want debug, info, warn, or error)", level)
	}
	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "pretty", "text":
		handler = slog.NewTextHandler(out, opts)
	case "json":
		handler = slog.NewJSONHandler(out, opts)
	default:
		return nil, fmt.Errorf("unknown log format %q (want pretty or json)", format)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, nil
}
