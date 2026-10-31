package model

import "encoding/json"

// Native histories are lossless. Known projections are decoded once at the
// database/RPC boundary; the original native document preserves fields belonging
// to Harness and future event variants that Nimbus must never rewrite.
type NativeHeader struct {
	Version         int64  `json:"version"`
	ID              string `json:"id"`
	CreatedAt       int64  `json:"createdAt"`
	Cwd             string `json:"cwd,omitempty"`
	IsSeeded        bool   `json:"isSeeded"`
	AgentPreset     string `json:"agentPreset,omitempty"`
	ParentSession   string `json:"parentSession,omitempty"`
	Origin          string `json:"origin,omitempty"`
	DelegationDepth int64  `json:"delegationDepth,omitempty"`
	original        json.RawMessage
}
type NativeRecord struct {
	Seq      int64     `json:"seq"`
	Type     string    `json:"type"`
	Time     int64     `json:"time,omitempty"`
	Data     EventData `json:"data"`
	original json.RawMessage
}
type EventData struct {
	Active *bool `json:"active,omitempty"`
}
type FrameChunk struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
type FramePayload struct {
	Type      string      `json:"type"`
	AttemptID string      `json:"attemptId,omitempty"`
	Chunk     *FrameChunk `json:"chunk,omitempty"`
	CallID    string      `json:"callId,omitempty"`
	Channel   string      `json:"channel,omitempty"`
	Text      string      `json:"text,omitempty"`
	original  json.RawMessage
}

func (v *NativeHeader) UnmarshalJSON(raw []byte) error {
	type fields NativeHeader
	var decoded fields
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = NativeHeader(decoded)
	v.original = append(json.RawMessage(nil), raw...)
	return nil
}
func (v NativeHeader) MarshalJSON() ([]byte, error) {
	if len(v.original) > 0 {
		return v.original, nil
	}
	type fields NativeHeader
	return json.Marshal(fields(v))
}

func (v *NativeRecord) UnmarshalJSON(raw []byte) error {
	type fields NativeRecord
	var decoded fields
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = NativeRecord(decoded)
	v.original = append(json.RawMessage(nil), raw...)
	return nil
}
func (v NativeRecord) MarshalJSON() ([]byte, error) {
	if len(v.original) > 0 {
		return v.original, nil
	}
	type fields NativeRecord
	return json.Marshal(fields(v))
}

func (v *FramePayload) UnmarshalJSON(raw []byte) error {
	type fields FramePayload
	var decoded fields
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = FramePayload(decoded)
	v.original = append(json.RawMessage(nil), raw...)
	return nil
}
func (v FramePayload) MarshalJSON() ([]byte, error) {
	if len(v.original) > 0 {
		return v.original, nil
	}
	type fields FramePayload
	return json.Marshal(fields(v))
}

func (v *SandboxInfo) UnmarshalJSON(raw []byte) error {
	type fields SandboxInfo
	var decoded fields
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = SandboxInfo(decoded)
	v.original = append(json.RawMessage(nil), raw...)
	return nil
}
func (v SandboxInfo) MarshalJSON() ([]byte, error) {
	if len(v.original) > 0 {
		return v.original, nil
	}
	type fields SandboxInfo
	return json.Marshal(fields(v))
}

func (v *OAuthClient) UnmarshalJSON(raw []byte) error {
	type fields OAuthClient
	var decoded fields
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = OAuthClient(decoded)
	v.original = append(json.RawMessage(nil), raw...)
	return nil
}
func (v OAuthClient) MarshalJSON() ([]byte, error) {
	if len(v.original) > 0 {
		return v.original, nil
	}
	type fields OAuthClient
	return json.Marshal(fields(v))
}

func (v *OAuthTokens) UnmarshalJSON(raw []byte) error {
	type fields OAuthTokens
	var decoded fields
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = OAuthTokens(decoded)
	v.original = append(json.RawMessage(nil), raw...)
	return nil
}
func (v OAuthTokens) MarshalJSON() ([]byte, error) {
	if len(v.original) > 0 {
		return v.original, nil
	}
	type fields OAuthTokens
	return json.Marshal(fields(v))
}

func (v *MCPContent) UnmarshalJSON(raw []byte) error {
	type fields MCPContent
	var decoded fields
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = MCPContent(decoded)
	v.original = append(json.RawMessage(nil), raw...)
	return nil
}
func (v MCPContent) MarshalJSON() ([]byte, error) {
	if len(v.original) > 0 {
		return v.original, nil
	}
	type fields MCPContent
	return json.Marshal(fields(v))
}

func (v *MCPCallResult) UnmarshalJSON(raw []byte) error {
	type fields MCPCallResult
	var decoded fields
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = MCPCallResult(decoded)
	v.original = append(json.RawMessage(nil), raw...)
	return nil
}
func (v MCPCallResult) MarshalJSON() ([]byte, error) {
	if len(v.original) > 0 {
		return v.original, nil
	}
	type fields MCPCallResult
	return json.Marshal(fields(v))
}
