package sandbox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
)

// Observe control API and envd RPC traffic without recording credentials, query
// strings, request/response bodies, commands, files or terminal contents.
type diagnosticTransport struct {
	base http.RoundTripper
	key  string
}

func (t *diagnosticTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	started := time.Now()
	logger := logging.FromContext(req.Context()).With("provider", "e2b", "method", req.Method, "host", req.URL.Hostname(), "path", req.URL.EscapedPath())
	logger.DebugContext(req.Context(), "e2b request started")
	trace := &httptrace.ClientTrace{
		DNSDone: func(info httptrace.DNSDoneInfo) {
			logger.Debug("e2b DNS completed", "address_count", len(info.Addrs), "error", t.safeError(info.Err))
		},
		ConnectDone: func(network, addr string, err error) {
			logger.Debug("e2b connection completed", "network", network, "address", addr, "error", t.safeError(err))
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			logger.Debug("e2b TLS handshake completed", "tls_version", tls.VersionName(state.Version), "verified_chains", len(state.VerifiedChains), "error_kind", networkErrorKind(err), "error", t.safeError(err))
		},
		GotConn: func(info httptrace.GotConnInfo) {
			logger.Debug("e2b connection acquired", "reused", info.Reused)
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	response, err := t.base.RoundTrip(req)
	attrs := []any{"duration_ms", time.Since(started).Milliseconds()}
	if err != nil {
		level := slog.LevelError
		if errors.Is(err, context.Canceled) {
			level = slog.LevelDebug
		}
		logger.Log(req.Context(), level, "e2b request failed", append(attrs, "error_kind", networkErrorKind(err), "error", t.safeError(err))...)
		return response, err
	}
	level := slog.LevelDebug
	if response.StatusCode >= 500 {
		level = slog.LevelError
	} else if response.StatusCode >= 400 {
		level = slog.LevelWarn
	}
	logger.Log(req.Context(), level, "e2b response headers received", append(attrs, "status", response.StatusCode)...)
	return response, nil
}

func (t *diagnosticTransport) safeError(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), t.key, "[REDACTED]")
}

func networkErrorKind(err error) string {
	if err == nil {
		return ""
	}
	var authority x509.UnknownAuthorityError
	var certificate *tls.CertificateVerificationError
	var dns *net.DNSError
	var network net.Error
	switch {
	case errors.As(err, &authority):
		return "tls_unknown_authority"
	case errors.As(err, &certificate):
		return "tls_certificate_verification"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &network) && network.Timeout():
		return "timeout"
	default:
		return "network"
	}
}
