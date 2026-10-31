package executor

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/runtimeprotocol/generated"
)

func optional[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}
func value[T any](v *T) (out T) {
	if v != nil {
		return *v
	}
	return
}
func turnRequest(input TurnInput) (generated.TurnRequest, error) {
	wire := generated.TurnRequest{SessionID: input.SessionID, TurnID: input.TurnID, MessageID: input.MessageID, Message: input.Message, SessionMode: generated.Mode(input.SessionMode), WorkDir: input.WorkDir, APIKey: input.APIKey, Recovery: optional(input.Recovery), Options: generated.TurnOptions{Model: generated.Model(input.Options.Model), Mode: generated.Mode(input.Options.Mode), Thinking: generated.Thinking(input.Options.Thinking), Plan: input.Options.Plan}, Records: []json.RawMessage{}, Skills: []generated.Skill{}, MCP: []generated.MCPTool{}, Images: []generated.ImageInput{}, Attachments: []generated.Attachment{}}
	var err error
	wire.Header, err = json.Marshal(input.Header)
	if err != nil {
		return wire, err
	}
	for _, record := range input.Records {
		raw, err := json.Marshal(record)
		if err != nil {
			return wire, err
		}
		wire.Records = append(wire.Records, raw)
	}
	for _, skill := range input.Skills {
		files := make([]generated.SkillFile, 0, len(skill.Files))
		for _, file := range skill.Files {
			files = append(files, generated.SkillFile{Path: file.Path, Content: file.Content})
		}
		wire.Skills = append(wire.Skills, generated.Skill{ID: skill.ID, Name: skill.Name, Description: skill.Description, Files: files})
	}
	for _, tool := range input.MCP {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return wire, err
		}
		wire.MCP = append(wire.MCP, generated.MCPTool{Name: optional(tool.Name), Description: optional(tool.Description), InputSchema: schema, Server: optional(tool.Server), Error: optional(tool.Error)})
	}
	for _, image := range input.Images {
		ref, err := json.Marshal(image.Attachment)
		if err != nil {
			return wire, err
		}
		wire.Images = append(wire.Images, generated.ImageInput{ID: optional(image.ID), Name: optional(image.Name), URL: optional(image.URL), Attachment: ref})
	}
	for _, attachment := range input.Attachments {
		ref, err := json.Marshal(attachment.Ref)
		if err != nil {
			return wire, err
		}
		wire.Attachments = append(wire.Attachments, generated.Attachment{Ref: ref, Data: base64.StdEncoding.EncodeToString(attachment.Data)})
	}
	return wire, nil
}
func oauthRequest(input model.OAuthData) (generated.OAuthData, error) {
	client, err := json.Marshal(input.Client)
	if err != nil {
		return generated.OAuthData{}, err
	}
	tokens, err := json.Marshal(input.Tokens)
	return generated.OAuthData{State: optional(input.State), ExpiresAt: optional(input.ExpiresAt), Verifier: optional(input.Verifier), Client: client, Tokens: tokens}, err
}
func mcpResult(wire generated.MCPResult) (model.MCPResult, error) {
	result := model.MCPResult{OK: wire.Ok, AuthURL: value(wire.AuthURL), Error: value(wire.Error), OAuthState: wire.OauthState, Tools: []model.MCPTool{}, OAuthData: model.OAuthData{State: value(wire.OauthData.State), ExpiresAt: value(wire.OauthData.ExpiresAt), Verifier: value(wire.OauthData.Verifier)}}
	if len(wire.OauthData.Client) > 0 {
		if err := json.Unmarshal(wire.OauthData.Client, &result.OAuthData.Client); err != nil {
			return result, err
		}
	}
	if len(wire.OauthData.Tokens) > 0 {
		if err := json.Unmarshal(wire.OauthData.Tokens, &result.OAuthData.Tokens); err != nil {
			return result, err
		}
	}
	if len(wire.Result) > 0 {
		if err := json.Unmarshal(wire.Result, &result.Result); err != nil {
			return result, err
		}
	}
	for _, tool := range wire.Tools {
		var schema model.DynamicJSON
		if len(tool.InputSchema) > 0 {
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				return result, err
			}
		}
		result.Tools = append(result.Tools, model.MCPTool{Name: value(tool.Name), Description: value(tool.Description), Server: value(tool.Server), Error: value(tool.Error), InputSchema: schema})
	}
	return result, nil
}
func encodeToolResult(result model.ToolResult) (json.RawMessage, error) {
	count := 0
	for _, set := range []bool{result.Sandbox != nil, result.Command != nil, result.ReadFile != nil, result.WriteFile != nil, result.MCP != nil} {
		if set {
			count++
		}
	}
	if count != 1 {
		return nil, errors.New("tool result must contain exactly one variant")
	}
	switch {
	case result.Sandbox != nil:
		return json.Marshal(result.Sandbox)
	case result.Command != nil:
		return json.Marshal(result.Command)
	case result.ReadFile != nil:
		return json.Marshal(result.ReadFile)
	case result.WriteFile != nil:
		return json.Marshal(result.WriteFile)
	default:
		return json.Marshal(result.MCP)
	}
}
