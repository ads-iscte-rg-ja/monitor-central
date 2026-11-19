package observability

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"strings"
)

// NewLogger builds the structured logger (format "json" or "text"; level
// "debug", "info", "warn" or "error"), makes it the slog default, and returns a
// *log.Logger that writes through it, so existing log.Printf calls come out in
// the same format without being rewritten.
func NewLogger(w io.Writer, format, level string) (*slog.Logger, *log.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.TrimSpace(level))); err != nil {
		return nil, nil, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error")
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		handler = slog.NewJSONHandler(w, opts)
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		return nil, nil, fmt.Errorf("LOG_FORMAT must be json or text")
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, slog.NewLogLogger(handler, slog.LevelInfo), nil
}
