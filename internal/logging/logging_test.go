package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"net/url"
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

func TestURLsAndNestedValuesCannotBypassRedaction(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewHandler(&out, slog.LevelDebug, "known-key", "postgres://user:p%40ssword@example.test/db"))
	logger.Error("failed", "error", &url.Error{Op: "Get", URL: "https://user:unregistered-password@example.test/files?path=private-name&token=unknown-token#private-fragment", Err: errors.New("connection reset")})
	logger.Info("nested", "data", map[string]any{"array": []any{map[string]string{"value": "known-key", "url": "wss://example.test/pty?token=nested-token"}}}, "encoded_password", "p%40ssword",
		slog.Group("group", "uri", "https://example.test:bad/files?token=malformed-token"))
	logger.Info("credentials", "authorization", "unknown-header-credential", "object", map[string]string{"access_token": "unregistered-access-token"})
	text := out.String()
	for _, secret := range []string{"unregistered-password", "private-name", "unknown-token", "private-fragment", "known-key", "nested-token", "p%40ssword", "malformed-token", "unknown-header-credential", "unregistered-access-token"} {
		if strings.Contains(text, secret) {
			t.Errorf("log retained %q", secret)
		}
	}
	if !strings.Contains(text, "example.test/files") || !strings.Contains(text, "[REDACTED]") {
		t.Fatal(text)
	}
}

func TestRegisteredSettingsCredentialsAreRedacted(t *testing.T) {
	old := configuredRedactor.Load()
	r := newRedactor()
	configuredRedactor.Store(r)
	defer configuredRedactor.Store(old)
	var out bytes.Buffer
	logger := slog.New(newHandler(&out, slog.LevelDebug, r))
	RegisterSecrets("rotated-model-key")
	logger.Error("panic", "error", errors.New("provider rejected rotated-model-key"))
	if strings.Contains(out.String(), "rotated-model-key") || !strings.Contains(out.String(), "[REDACTED]") {
		t.Fatal(out.String())
	}
}

func TestConfigureRejectsInvalidLogLevel(t *testing.T) {
	if err := Configure("verbose-everything"); err == nil {
		t.Fatal("accepted invalid log level")
	}
}
