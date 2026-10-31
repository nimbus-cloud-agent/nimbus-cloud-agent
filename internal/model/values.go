// Package model contains application models and value objects, with no database
// or transport SDK dependencies. Codecs here are used only at IO boundaries.
package model

import (
	"encoding/json"
	"errors"
)

type Options struct {
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
	Mode     string `json:"mode"`
	Plan     bool   `json:"plan"`
}

func DefaultOptions() Options {
	return Options{Model: "deepseek-flash", Thinking: "off", Mode: "standard"}
}

type ImageDimensions struct {
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
}
type ImageRef struct {
	AttachmentID       string           `json:"attachmentId"`
	MediaType          string           `json:"mediaType"`
	Bytes              int64            `json:"bytes"`
	Width              int64            `json:"width"`
	Height             int64            `json:"height"`
	Name               string           `json:"name,omitempty"`
	OriginalDimensions *ImageDimensions `json:"originalDimensions,omitempty"`
}
type MessageImage struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Attachment ImageRef `json:"attachment"`
}
type Attachment struct {
	Ref  ImageRef
	Data []byte
}

type SandboxInfo struct {
	SandboxID   string            `json:"sandboxId"`
	TemplateID  string            `json:"templateId,omitempty"`
	Name        string            `json:"name,omitempty"`
	State       string            `json:"state,omitempty"`
	Region      string            `json:"region,omitempty"`
	CPUCount    int64             `json:"cpuCount,omitempty"`
	MemoryMB    int64             `json:"memoryMB,omitempty"`
	StartedAt   string            `json:"startedAt,omitempty"`
	EndAt       string            `json:"endAt,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	EnvdVersion string            `json:"envdVersion,omitempty"`
	original    json.RawMessage
}
type CommandResult struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int64  `json:"exitCode"`
	Truncated bool   `json:"truncated"`
}
type FileReadResult struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}
type FileWriteResult struct {
	Path    string `json:"path"`
	Written bool   `json:"written"`
}
type CommandOutput struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}
type TerminalOutput struct {
	Data  string `json:"data,omitempty"`
	Ready bool   `json:"ready,omitempty"`
}
type TerminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Rows int64  `json:"rows,omitempty"`
	Cols int64  `json:"cols,omitempty"`
}
type SandboxArgs struct {
	Command   string `json:"command,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	TimeoutMS int64  `json:"timeoutMs,omitempty"`
	Path      string `json:"path,omitempty"`
	Content   string `json:"content,omitempty"`
}

// DynamicJSON represents provider-defined JSON (MCP argument/schema/content),
// not Nimbus fields. It has no late Decode/Get escape hatch: only IO codecs may
// construct it, and application code forwards it without inspecting its shape.
type DynamicJSON struct{ raw json.RawMessage }

func (d *DynamicJSON) UnmarshalJSON(raw []byte) error {
	if !json.Valid(raw) {
		return errors.New("invalid provider JSON")
	}
	d.raw = append(d.raw[:0], raw...)
	return nil
}
func (d DynamicJSON) MarshalJSON() ([]byte, error) {
	if len(d.raw) == 0 {
		return []byte("null"), nil
	}
	return d.raw, nil
}
func (d DynamicJSON) IsNull() bool { return len(d.raw) == 0 || string(d.raw) == "null" }

type OAuthData struct {
	State     string       `json:"state,omitempty"`
	ExpiresAt int64        `json:"expires_at,omitempty"`
	Verifier  string       `json:"verifier,omitempty"`
	Client    *OAuthClient `json:"client,omitempty"`
	Tokens    *OAuthTokens `json:"tokens,omitempty"`
}
type OAuthClient struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret,omitempty"`
	RedirectURIs []string `json:"redirect_uris,omitempty"`
	original     json.RawMessage
}
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
	original     json.RawMessage
}
type MCPTool struct {
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	InputSchema DynamicJSON `json:"inputSchema"`
	Server      string      `json:"server,omitempty"`
	Error       string      `json:"error,omitempty"`
}
type MCPArguments struct {
	Name      string      `json:"name,omitempty"`
	Arguments DynamicJSON `json:"arguments,omitempty"`
	Code      string      `json:"code,omitempty"`
}
type MCPContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	original json.RawMessage
}
type MCPCallResult struct {
	Content           []MCPContent `json:"content"`
	IsError           bool         `json:"isError,omitempty"`
	StructuredContent DynamicJSON  `json:"structuredContent,omitempty"`
	original          json.RawMessage
}
type MCPResult struct {
	OK         bool           `json:"ok"`
	AuthURL    string         `json:"auth_url,omitempty"`
	OAuthState string         `json:"oauth_state"`
	OAuthData  OAuthData      `json:"-"`
	Tools      []MCPTool      `json:"tools,omitempty"`
	Result     *MCPCallResult `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}
type QuestionIntent struct {
	Kind    string `json:"kind"`
	Approve string `json:"approve"`
	CallID  string `json:"callId,omitempty"`
}
type Question struct {
	ID          string           `json:"id"`
	Question    string           `json:"question"`
	Detail      string           `json:"detail,omitempty"`
	Header      string           `json:"header,omitempty"`
	Options     []QuestionOption `json:"options,omitempty"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
	Intent      *QuestionIntent  `json:"intent,omitempty"`
}
type QuestionRequest struct {
	Questions []Question `json:"questions"`
}
type AnswerItem struct {
	ID       string   `json:"id"`
	Selected []string `json:"selected"`
	Custom   *string  `json:"custom,omitempty"`
}
type QuestionAnswer struct {
	Dismissed bool         `json:"dismissed,omitempty"`
	Answers   []AnswerItem `json:"answers,omitempty"`
}

type ToolCall struct {
	Name    string
	CallID  string
	Sandbox SandboxArgs
	MCP     DynamicJSON
}

// A result is selected by the application, then encoded by the RPC adapter.
// No application operation serializes a result in order to pass it to another.
type ToolResult struct {
	Sandbox   *SandboxView
	Command   *CommandResult
	ReadFile  *FileReadResult
	WriteFile *FileWriteResult
	MCP       *MCPCallResult
}
type TurnDone struct {
	Seq     int64  `json:"seq"`
	Outcome string `json:"outcome"`
	Error   string `json:"error"`
}
type TurnEvent struct {
	Kind   string
	Header *NativeHeader
	Record *NativeRecord
	Frame  *FramePayload
	Done   *TurnDone
}
