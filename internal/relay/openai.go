package relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req OpenAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	bedrockModel, exposedModel, err := modelFromRequest(req.Model, s.config.Models)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	anthropicReq, err := translateOpenAIToAnthropic(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := json.Marshal(anthropicReq)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Stream {
		s.streamOpenAI(w, r.Context(), bedrockModel, exposedModel, body)
		return
	}

	resp, err := invokeAnthropic(r.Context(), s.bedrock, bedrockModel, body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, translateAnthropicToOpenAI(resp, exposedModel))
}

func translateOpenAIToAnthropic(req OpenAIChatRequest) (map[string]any, error) {
	out := map[string]any{
		"anthropic_version": anthropicVersion,
		"max_tokens":        maxOutputTokens(req),
		"messages":          []map[string]any{},
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if len(req.Stop) > 0 {
		out["stop_sequences"] = req.Stop
	}
	if len(req.Tools) > 0 {
		out["tools"] = openAIToolsToAnthropic(req.Tools)
	}
	if req.ToolChoice != nil {
		toolChoice, err := openAIToolChoiceToAnthropic(req.ToolChoice)
		if err != nil {
			return nil, err
		}
		if toolChoice != nil {
			out["tool_choice"] = toolChoice
		}
	}
	if effort, err := openAIEffortLevel(req); err != nil {
		return nil, err
	} else if effort != "" {
		out["output_config"] = map[string]any{"effort": effort}
		out["anthropic_beta"] = []string{effortBeta}
	}

	var systemParts []string
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system", "developer":
			text := textFromOpenAIContent(msg.Content)
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			content, err := openAIContentToAnthropic(msg.Content)
			if err != nil {
				return nil, err
			}
			messages = append(messages, map[string]any{"role": "user", "content": content})
		case "assistant":
			content, err := assistantContentToAnthropic(msg)
			if err != nil {
				return nil, err
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": content})
		case "tool":
			text := textFromOpenAIContent(msg.Content)
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": msg.ToolCallID,
					"content":     text,
				}},
			})
		default:
			return nil, fmt.Errorf("unsupported message role %q", msg.Role)
		}
	}
	if len(systemParts) > 0 {
		out["system"] = strings.Join(systemParts, "\n\n")
	}
	out["messages"] = messages
	return out, nil
}

func openAIEffortLevel(req OpenAIChatRequest) (string, error) {
	for _, raw := range []string{req.EffortLevel, req.EffortLevelSnake, req.ReasoningEffort} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		return normalizeEffortLevel(raw)
	}
	if req.Reasoning != nil {
		return normalizeEffortLevel(req.Reasoning.Effort)
	}
	return "", nil
}

func maxOutputTokens(req OpenAIChatRequest) int {
	switch {
	case req.MaxCompletionTokens > 0:
		return req.MaxCompletionTokens
	case req.MaxTokens > 0:
		return req.MaxTokens
	default:
		return defaultMaxTokens
	}
}

func openAIToolsToAnthropic(tools []OpenAITool) []AnthropicTool {
	out := make([]AnthropicTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		out = append(out, AnthropicTool{
			Type:        "custom",
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
	}
	return out
}

func openAIToolChoiceToAnthropic(choice any) (any, error) {
	switch v := choice.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]any{"type": "auto"}, nil
		case "none":
			return map[string]any{"type": "none"}, nil
		case "required":
			return map[string]any{"type": "any"}, nil
		default:
			return nil, fmt.Errorf("unsupported tool_choice %q", v)
		}
	case map[string]any:
		if v["type"] != "function" {
			return nil, fmt.Errorf("unsupported tool_choice type %v", v["type"])
		}
		fn, ok := v["function"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool_choice function is required")
		}
		name, ok := fn["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("tool_choice function.name is required")
		}
		return map[string]any{"type": "tool", "name": name}, nil
	default:
		return nil, fmt.Errorf("unsupported tool_choice")
	}
}

func assistantContentToAnthropic(msg OpenAIMessage) ([]map[string]any, error) {
	content, err := openAIContentToAnthropic(msg.Content)
	if err != nil {
		return nil, err
	}
	for _, call := range msg.ToolCalls {
		input := map[string]any{}
		if call.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(call.Function.Arguments), &input)
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    call.ID,
			"name":  call.Function.Name,
			"input": input,
		})
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": ""})
	}
	return content, nil
}

func openAIContentToAnthropic(raw json.RawMessage) ([]map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return []map[string]any{{"type": "text", "text": text}}, nil
	}

	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url,omitempty"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": part.Text})
		case "image_url":
			if part.ImageURL == nil {
				return nil, fmt.Errorf("image_url content missing url")
			}
			source, err := dataURLToAnthropicSource(part.ImageURL.URL)
			if err != nil {
				return nil, err
			}
			out = append(out, map[string]any{"type": "image", "source": source})
		}
	}
	return out, nil
}

func dataURLToAnthropicSource(url string) (map[string]any, error) {
	const marker = ";base64,"
	if !strings.HasPrefix(url, "data:") || !strings.Contains(url, marker) {
		return nil, fmt.Errorf("only base64 data: image URLs are supported")
	}
	trimmed := strings.TrimPrefix(url, "data:")
	parts := strings.SplitN(trimmed, marker, 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid data URL")
	}
	if _, err := base64.StdEncoding.DecodeString(parts[1]); err != nil {
		return nil, fmt.Errorf("invalid base64 image data: %w", err)
	}
	return map[string]any{
		"type":       "base64",
		"media_type": parts[0],
		"data":       parts[1],
	}, nil
}

func textFromOpenAIContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var text string
		_ = json.Unmarshal(raw, &text)
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return string(raw)
	}
	var texts []string
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func translateAnthropicToOpenAI(resp AnthropicResponse, model string) OpenAIChatResponse {
	var text strings.Builder
	var toolCalls []OpenAIToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, OpenAIToolCall{
				ID:   block.ID,
				Type: "function",
				Function: OpenAIFunctionCall{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		}
	}
	rawContent, _ := json.Marshal(text.String())
	finish := mapAnthropicStopToOpenAI(resp.StopReason)
	msg := OpenAIMessage{Role: "assistant", Content: rawContent, ToolCalls: toolCalls}
	return OpenAIChatResponse{
		ID:      responseID(resp.ID),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []OpenAIChoice{{Index: 0, Message: &msg, FinishReason: &finish}},
		Usage:   openAIUsage(resp.Usage),
	}
}

func mapAnthropicStopToOpenAI(reason string) string {
	switch reason {
	case "max_tokens", "model_context_window_exceeded":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence":
		return "stop"
	default:
		return "stop"
	}
}

func openAIUsage(usage AnthropicUsage) *OpenAIUsage {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}
	return &OpenAIUsage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.InputTokens + usage.OutputTokens,
	}
}

func responseID(id string) string {
	if id != "" {
		return id
	}
	return "chatcmpl-" + time.Now().Format("20060102150405")
}

func (s *Server) streamOpenAI(w http.ResponseWriter, ctx context.Context, modelID string, exposedModel string, body []byte) {
	chunks, errs, err := s.bedrock.Stream(ctx, modelID, body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	state := newOpenAIStreamState(exposedModel)
	writeSSEJSON(w, flusher, state.roleChunk())
	for chunk := range chunks {
		var ev AnthropicStreamEvent
		if err := json.Unmarshal(chunk, &ev); err != nil {
			continue
		}
		for _, out := range state.translate(ev) {
			writeSSEJSON(w, flusher, out)
		}
	}
	if err := <-errs; err != nil {
		writeSSEJSON(w, flusher, apiError{Error: apiErrorBody{Message: err.Error(), Type: "api_error"}})
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}
