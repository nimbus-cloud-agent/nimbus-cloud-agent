package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var logURL = regexp.MustCompile(`[[:alpha:]][[:alnum:]+.-]*://[^[:space:]<>"']+`)

type redactor struct {
	mu      sync.RWMutex
	secrets []string
}

func newRedactor(secrets ...string) *redactor {
	r := &redactor{}
	r.add(secrets...)
	return r
}

func (r *redactor) add(secrets ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	add := func(secret string) {
		if secret == "" {
			return
		}
		for _, existing := range r.secrets {
			if existing == secret {
				return
			}
		}
		r.secrets = append(r.secrets, secret)
	}
	for _, secret := range secrets {
		add(secret)
		add(url.QueryEscape(secret))
		add(url.PathEscape(secret))
		if u, err := url.Parse(secret); err == nil && u.User != nil {
			if password, ok := u.User.Password(); ok {
				add(password)
				add(url.QueryEscape(password))
				add(url.PathEscape(password))
			}
		}
	}
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
}

func (r *redactor) text(text string) string {
	text = logURL.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			// Even malformed URLs must not retain query parameters.
			if index := strings.IndexAny(raw, "?#"); index >= 0 {
				raw = raw[:index] + "?[REDACTED]"
			}
			if scheme := strings.Index(raw, "://"); scheme >= 0 {
				if at := strings.IndexByte(raw[scheme+3:], '@'); at >= 0 {
					raw = raw[:scheme+3] + raw[scheme+3+at+1:]
				}
			}
			return raw
		}
		query, fragment := u.RawQuery != "" || u.ForceQuery, u.Fragment != ""
		u.User, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = nil, "", "", "", false
		text := u.String()
		if query {
			text += "?[REDACTED]"
		}
		if fragment {
			text += "#[REDACTED]"
		}
		return text
	})
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, secret := range r.secrets {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

// Sanitize also protects errors returned to clients, before they reach slog.
func Sanitize(text string, secrets ...string) string {
	text = newRedactor(secrets...).text(text)
	if r := configuredRedactor.Load(); r != nil {
		text = r.text(text)
	}
	return text
}

func (r *redactor) value(v slog.Value) slog.Value {
	switch v.Kind() {
	case slog.KindString:
		return slog.StringValue(r.text(v.String()))
	case slog.KindAny:
		switch value := v.Any().(type) {
		case error:
			return slog.StringValue(r.text(value.Error()))
		case fmt.Stringer:
			return slog.StringValue(r.text(value.String()))
		}
		raw, err := json.Marshal(v.Any())
		if err != nil {
			return slog.StringValue("[unserializable value]")
		}
		var decoded any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&decoded) != nil {
			return slog.StringValue("[unserializable value]")
		}
		return slog.AnyValue(r.object(decoded))
	default:
		return v
	}
}

func (r *redactor) object(value any) any {
	switch value := value.(type) {
	case string:
		return r.text(value)
	case []any:
		for i := range value {
			value[i] = r.object(value[i])
		}
		return value
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, value := range value {
			if sensitiveKey(key) {
				result[r.text(key)] = "[REDACTED]"
			} else {
				result[r.text(key)] = r.object(value)
			}
		}
		return result
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return key == "password" || key == "passwd" || key == "secret" || key == "api_key" ||
		key == "apikey" || key == "authorization" || key == "proxy_authorization" ||
		key == "cookie" || key == "set_cookie" || key == "token" ||
		strings.HasSuffix(key, "_password") || strings.HasSuffix(key, "_secret") ||
		strings.HasSuffix(key, "_token") || strings.HasSuffix(key, "_api_key")
}
