package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

type Loggers struct {
	App   *slog.Logger
	Audit *slog.Logger
}

func New(level, format string) (Loggers, error) {
	return NewWithWriters(level, format, os.Stdout, os.Stdout)
}

func NewWithWriters(level, format string, appWriter, auditWriter io.Writer) (Loggers, error) {
	logLevel, err := parseLevel(level)
	if err != nil {
		return Loggers{}, err
	}

	appHandler, err := newHandler(format, appWriter, logLevel)
	if err != nil {
		return Loggers{}, err
	}
	auditHandler, err := newHandler(format, auditWriter, logLevel)
	if err != nil {
		return Loggers{}, err
	}

	return Loggers{
		App:   slog.New(appHandler).With("log_type", "app"),
		Audit: slog.New(auditHandler).With("log_type", "audit"),
	}, nil
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", level)
	}
}

func newHandler(format string, writer io.Writer, level slog.Level) (slog.Handler, error) {
	options := &slog.HandlerOptions{Level: level}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return slog.NewJSONHandler(writer, options), nil
	case "text":
		return slog.NewTextHandler(writer, options), nil
	default:
		return nil, fmt.Errorf("unsupported log format %q", format)
	}
}

func Error(logger *slog.Logger, msg string, err error, attrs ...any) {
	if err == nil {
		logger.Error(msg, attrs...)
		return
	}
	attrs = append(attrs, "error", err.Error())
	logger.Error(msg, attrs...)
}

func InfoContext(ctx context.Context, logger *slog.Logger, msg string, attrs ...any) {
	logger.InfoContext(ctx, msg, attrs...)
}
