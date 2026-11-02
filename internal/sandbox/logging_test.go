package sandbox

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
)

func TestTLSFailureIsLoggedWithoutSecrets(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted TLS connection reached handler") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(logging.NewHandler(&out, slog.LevelDebug)))
	defer slog.SetDefault(previous)
	base := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()}}
	defer base.CloseIdleConnections()
	client := &http.Client{Transport: &diagnosticTransport{base: base, key: "secret-api-key"}}
	req, err := http.NewRequest("POST", server.URL+"/sandboxes?token=private-query", strings.NewReader("private-request-body"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", "secret-api-key")
	if response, err := client.Do(req); err == nil {
		response.Body.Close()
		t.Fatal("certificate verification was bypassed")
	}
	text := out.String()
	for _, secret := range []string{"secret-api-key", "private-query", "private-request-body"} {
		if strings.Contains(text, secret) {
			t.Fatalf("logged secret %q", secret)
		}
	}
	if !strings.Contains(text, `"error_kind":"tls_unknown_authority"`) || !strings.Contains(text, `"level":"ERROR"`) || !strings.Contains(text, "e2b TLS handshake completed") {
		t.Fatal(text)
	}
}
