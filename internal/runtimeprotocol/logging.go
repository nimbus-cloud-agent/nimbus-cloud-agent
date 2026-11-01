package runtimeprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
)

// SetLogger forwards SDK diagnostics only; LogMessages would expose RPC input,
// model credentials and tool results and must never be enabled.
type protocolLogger struct {
	ctx     context.Context
	closing *atomic.Bool
}

func (l protocolLogger) Printf(format string, args ...any) {
	log := logging.FromContext(l.ctx)
	if strings.HasPrefix(format, "jsonrpc2: protocol error:") && len(args) > 0 {
		if l.closing != nil && l.closing.Load() {
			return
		}
		attrs := []any{"component", "jsonrpc2"}
		if err, ok := args[0].(error); ok {
			attrs = append(attrs, "error_type", fmt.Sprintf("%T", err))
			var syntax *json.SyntaxError
			var mismatch *json.UnmarshalTypeError
			var number *strconv.NumError
			switch {
			case errors.As(err, &syntax):
				attrs = append(attrs, "error", "invalid JSON-RPC JSON", "offset", syntax.Offset)
			case errors.As(err, &mismatch):
				attrs = append(attrs, "error", "invalid JSON-RPC field type", "offset", mismatch.Offset)
			case errors.As(err, &number):
				attrs = append(attrs, "error", "invalid JSON-RPC frame length")
			default:
				attrs = append(attrs, "error", logging.Sanitize(err.Error()))
			}
		}
		log.ErrorContext(l.ctx, "runtime RPC protocol error", attrs...)
		return
	}
	// Unknown-response IDs and other SDK arguments are untrusted wire data.
	log.DebugContext(l.ctx, "runtime RPC diagnostic", "component", "jsonrpc2", "diagnostic", strings.TrimSpace(format))
}
