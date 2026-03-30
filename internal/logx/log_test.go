package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLogWrappers(t *testing.T) {
	oldLogger := logger
	var buf bytes.Buffer
	logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() {
		logger = oldLogger
	})

	Debug("debug message", "k", "v")
	Info("info message", "k", "v")
	Warn("warn message", "k", "v")
	Error("error message", "k", "v")

	out := buf.String()
	for _, msg := range []string{"debug message", "info message", "warn message", "error message"} {
		if !strings.Contains(out, msg) {
			t.Fatalf("expected output to contain %q, got %q", msg, out)
		}
	}
}
