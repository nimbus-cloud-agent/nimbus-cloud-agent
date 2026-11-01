package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestDebugLogsRedactConfiguredCredentials(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewHandler(&out, slog.LevelDebug, "test-api-key", "postgres://user:test-db-password@example.test/db"))
	logger.Debug("operation failed", "error", errors.New("test-api-key and test-db-password"))
	text := out.String()
	if !strings.Contains(text, `"level":"DEBUG"`) || !strings.Contains(text, "[REDACTED]") || strings.Contains(text, "test-api-key") || strings.Contains(text, "test-db-password") {
		t.Fatal(text)
	}
}

func TestConfigureRejectsInvalidLogLevel(t *testing.T) {
	if err := Configure("verbose-everything"); err == nil {
		t.Fatal("accepted invalid log level")
	}
}
