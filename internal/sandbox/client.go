package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	e2b "github.com/matiasinsaurralde/go-e2b"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

const outputLimit = 1 << 20

type Client struct {
	sdk    *e2b.Client
	key    string
	apiURL string
	http   *http.Client
}

func New(key string) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 20 * time.Second
	// No whole-request timeout: terminal response bodies remain open for the
	// PTY lifetime. Short operations are bounded with contexts instead.
	return newClient(key, e2b.DefaultAPIBaseURL, &http.Client{Transport: transport})
}
func newClient(key, apiURL string, httpClient *http.Client) (*Client, error) {
	if key == "" {
		return nil, errors.New("E2B API key is required")
	}
	sdk, err := e2b.NewClient(e2b.ClientConfig{APIKey: key, APIBaseURL: apiURL, HTTPClient: httpClient})
	if err != nil {
		return nil, fmt.Errorf("initialize E2B client: %s", strings.ReplaceAll(err.Error(), key, "[REDACTED]"))
	}
	return &Client{sdk: sdk, key: key, apiURL: apiURL, http: httpClient}, nil
}

// Translate provider errors at ingress. Preserve context cancellation for the
// application, but never expose SDK error types or credentials through RPC.
func (c *Client) failure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	message := err.Error()
	if c.key != "" {
		message = strings.ReplaceAll(message, c.key, "[REDACTED]")
	}
	return errors.New(message)
}

// The SDK only exposes Info/Pause/Kill on a connected handle. These small
// control-API adapters operate by ID without Connect, which would resume a
// paused sandbox. Data-plane operations use the SDK's generated Connect RPC.
type infoResponse struct {
	e2b.SandboxInfo
	Region string `json:"region"`
}
type apiFailure struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (c *Client) request(ctx context.Context, method, id, suffix string) (*http.Response, error) {
	if id == "" {
		return nil, errors.New("sandbox ID is required")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+"/sandboxes/"+url.PathEscape(id)+suffix, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.key)
	return c.http.Do(req)
}
func (c *Client) responseError(ctx context.Context, response *http.Response) error {
	var body apiFailure
	_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&body)
	return c.failure(ctx, fmt.Errorf("E2B returned HTTP %d: %s", response.StatusCode, body.Message))
}
func (c *Client) Info(ctx context.Context, id string) (model.SandboxInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	response, err := c.request(ctx, http.MethodGet, id, "")
	if err != nil {
		return model.SandboxInfo{}, c.failure(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return model.SandboxInfo{SandboxID: id, State: "killed"}, nil
	}
	if response.StatusCode != http.StatusOK {
		return model.SandboxInfo{}, c.responseError(ctx, response)
	}
	var info infoResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, outputLimit)).Decode(&info); err != nil {
		return model.SandboxInfo{}, c.failure(ctx, err)
	}
	if info.ID != id {
		return model.SandboxInfo{}, errors.New("E2B returned an unexpected sandbox identity")
	}
	return model.SandboxInfo{SandboxID: info.ID, TemplateID: info.Template, Name: info.Alias, State: info.State, Region: info.Region, CPUCount: int64(info.CPUCount), MemoryMB: int64(info.MemoryMB), StartedAt: info.StartedAt, EndAt: info.EndAt, Metadata: info.Metadata, EnvdVersion: info.EnvdVersion}, nil
}
func (c *Client) changeState(ctx context.Context, id, method, suffix string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	response, err := c.request(ctx, method, id, suffix)
	if err != nil {
		return c.failure(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if method == http.MethodDelete && response.StatusCode == http.StatusNotFound {
		return nil
	}
	return c.responseError(ctx, response)
}
func (c *Client) Pause(ctx context.Context, id string) error {
	return c.changeState(ctx, id, http.MethodPost, "/pause")
}
func (c *Client) Kill(ctx context.Context, id string) error {
	return c.changeState(ctx, id, http.MethodDelete, "")
}
func (c *Client) Create(ctx context.Context, template, sid string) (info model.SandboxInfo, err error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	sb, err := c.sdk.NewSandbox(ctx, e2b.SandboxConfig{Template: template, Timeout: 3600, Secure: true, AutoPause: true, AutoResume: &e2b.AutoResumeConfig{Enabled: false}, Metadata: map[string]string{"nimbus_session": sid}})
	if err != nil {
		return info, c.failure(ctx, err)
	}
	defer func() {
		if err != nil {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			_ = c.Kill(cleanup, sb.ID)
		}
	}()
	_, err = sb.Commands.Run(ctx, "mkdir -p /workspace && chmod 777 /workspace", e2b.WithUser("root"), e2b.WithTimeout(15*time.Second))
	if err != nil {
		return info, c.failure(ctx, err)
	}
	return c.Info(ctx, sb.ID)
}
func (c *Client) Connect(ctx context.Context, id string) (model.SandboxInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := c.sdk.Connect(ctx, id, 3600); err != nil {
		return model.SandboxInfo{}, c.failure(ctx, err)
	}
	return c.Info(ctx, id)
}
func (c *Client) ReadFile(ctx context.Context, id, path string) (model.FileReadResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	sb, err := c.sdk.Connect(ctx, id, 60)
	if err != nil {
		return model.FileReadResult{}, c.failure(ctx, err)
	}
	r, err := sb.Filesystem.Read(ctx, path, e2b.WithFileUser("root"))
	if err != nil {
		return model.FileReadResult{}, c.failure(ctx, err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, outputLimit+1))
	if err != nil {
		return model.FileReadResult{}, c.failure(ctx, err)
	}
	return model.FileReadResult{Content: strings.ToValidUTF8(string(data[:min(len(data), outputLimit)]), "�"), Truncated: len(data) > outputLimit}, nil
}
func (c *Client) WriteFile(ctx context.Context, id, path, content string) (model.FileWriteResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	sb, err := c.sdk.Connect(ctx, id, 60)
	if err != nil {
		return model.FileWriteResult{}, c.failure(ctx, err)
	}
	_, err = sb.Filesystem.Write(ctx, path, strings.NewReader(content), e2b.WithFileUser("root"))
	if err != nil {
		return model.FileWriteResult{}, c.failure(ctx, err)
	}
	return model.FileWriteResult{Path: path, Written: true}, nil
}
