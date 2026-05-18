package logging

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type stdlibWriter struct {
	logger *slog.Logger
}

func (w stdlibWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		w.logger.Debug(msg)
	}
	return len(p), nil
}

func ParseLevel(in string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q (expected debug|info|warn|error)", in)
	}
}

func Setup(level string) (*slog.Logger, io.Closer, string, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return nil, nil, "", err
	}

	logPath := "/tmp/cogitator.log"
	if stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); stateHome != "" {
		candidate := filepath.Join(stateHome, "cogitator", "cogitator.log")
		if mkErr := os.MkdirAll(filepath.Dir(candidate), 0o755); mkErr == nil {
			logPath = candidate
		}
	}

	open := func(path string) (*os.File, error) {
		return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	}

	f, err := open(logPath)
	if err != nil && logPath != "/tmp/cogitator.log" {
		logPath = "/tmp/cogitator.log"
		f, err = open(logPath)
	}
	if err != nil {
		return nil, nil, logPath, err
	}

	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: lvl}))
	log.SetFlags(0)
	log.SetOutput(stdlibWriter{logger: logger})
	return logger, f, logPath, nil
}
