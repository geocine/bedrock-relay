package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	normalizeResponsesToolsForLlamaCpp(raw)
	hoistResponsesInstructionMessagesForLlamaCpp(raw)

	bedrockModel, exposedModel, err := modelFromRequest(stringValue(raw["model"]), s.config.Models)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	anthropicReq, err := translateResponsesToAnthropic(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := json.Marshal(anthropicReq)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if boolValue(raw["stream"]) {
		s.streamResponses(w, r.Context(), bedrockModel, exposedModel, body)
		return
	}

	resp, err := invokeAnthropic(r.Context(), s.bedrock, bedrockModel, body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, translateAnthropicToResponses(resp, exposedModel))
}

func translateResponsesToAnthropic(req map[string]any) (map[string]any, error) {
	out := map[string]any{
		"anthropic_version": anthropicVersion,
		"max_tokens":        responsesMaxOutputTokens(req),
		"messages":          []map[string]any{},
	}
	if temperature, ok := req["temperature"].(float64); ok {
		out["temperature"] = temperature
	}
	if topP, ok := req["top_p"].(float64); ok {
		out["top_p"] = topP
	}
	if instructions := stringValue(req["instructions"]); instructions != "" {
		out["system"] = instructions
	}
	if tools, err := responsesToolsToAnthropic(req["tools"]); err != nil {
		return nil, err
	} else if len(tools) > 0 {
		out["tools"] = tools
	}
	if toolChoice, err := responsesToolChoiceToAnthropic(req["tool_choice"]); err != nil {
		return nil, err
	} else if toolChoice != nil {
		out["tool_choice"] = toolChoice
	}
	if effort, err := responsesEffortLevel(req); err != nil {
		return nil, err
	} else if effort != "" {
		out["output_config"] = map[string]any{"effort": effort}
		out["anthropic_beta"] = []string{effortBeta}
	}

	messages, err := responsesInputToAnthropicMessages(req["input"])
	if err != nil {
		return nil, err
	}
	messages = removeAssistantPrefill(messages)
	out["messages"] = messages
	return out, nil
}

func responsesMaxOutputTokens(req map[string]any) int {
	for _, key := range []string{"max_output_tokens", "max_tokens"} {
		if value := intValue(req[key]); value > 0 {
			return value
		}
	}
	return defaultMaxTokens
}

func responsesEffortLevel(req map[string]any) (string, error) {
	if reasoning, ok := req["reasoning"].(map[string]any); ok {
		if effort := stringValue(reasoning["effort"]); effort != "" {
			return normalizeResponsesEffortLevel(effort)
		}
	}
	return "", nil
}

func responsesToolsToAnthropic(raw any) ([]AnthropicTool, error) {
	rawTools, ok := raw.([]any)
	if !ok {
		return nil, nil
	}
	tools := make([]AnthropicTool, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]any)
		if !ok || toolType(tool) != "function" {
			continue
		}
		name := stringValue(tool["name"])
		if name == "" {
			return nil, fmt.Errorf("function tool name is required")
		}
		parameters, _ := tool["parameters"].(map[string]any)
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, AnthropicTool{
			Type:        "custom",
			Name:        name,
			Description: stringValue(tool["description"]),
			InputSchema: parameters,
		})
	}
	return tools, nil
}

func responsesToolChoiceToAnthropic(raw any) (any, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case string:
		return openAIToolChoiceToAnthropic(value)
	case map[string]any:
		switch toolType(value) {
		case "auto":
			return map[string]any{"type": "auto"}, nil
		case "none":
			return map[string]any{"type": "none"}, nil
		case "required":
			return map[string]any{"type": "any"}, nil
		case "function":
			name := stringValue(value["name"])
			if name == "" {
				return nil, fmt.Errorf("tool_choice function name is required")
			}
			return map[string]any{"type": "tool", "name": name}, nil
		default:
			return nil, fmt.Errorf("unsupported tool_choice type %q", toolType(value))
		}
	default:
		return nil, fmt.Errorf("unsupported tool_choice")
	}
}

func responsesInputToAnthropicMessages(raw any) ([]map[string]any, error) {
	switch input := raw.(type) {
	case string:
		return []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": input}}}}, nil
	case []any:
		messages := make([]map[string]any, 0, len(input))
		for _, rawItem := range input {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			switch toolType(item) {
			case "message":
				message, err := responsesMessageToAnthropic(item)
				if err != nil {
					return nil, err
				}
				if message != nil {
					messages = append(messages, message)
				}
			case "function_call":
				content, err := responsesFunctionCallToAnthropic(item)
				if err != nil {
					return nil, err
				}
				messages = append(messages, map[string]any{"role": "assistant", "content": []map[string]any{content}})
			case "function_call_output":
				messages = append(messages, responsesFunctionOutputToAnthropic(item))
			}
		}
		return messages, nil
	default:
		return nil, fmt.Errorf("input is required")
	}
}

func responsesMessageToAnthropic(item map[string]any) (map[string]any, error) {
	role := strings.ToLower(strings.TrimSpace(stringValue(item["role"])))
	switch role {
	case "user", "assistant":
	default:
		return nil, nil
	}
	content, err := responsesContentToAnthropic(item["content"])
	if err != nil {
		return nil, err
	}
	return map[string]any{"role": role, "content": content}, nil
}

func responsesContentToAnthropic(raw any) ([]map[string]any, error) {
	switch content := raw.(type) {
	case string:
		return []map[string]any{{"type": "text", "text": content}}, nil
	case []any:
		parts := make([]map[string]any, 0, len(content))
		for _, rawPart := range content {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			switch toolType(part) {
			case "input_text", "output_text", "text":
				parts = append(parts, map[string]any{"type": "text", "text": rawStringValue(part["text"])})
			case "input_image":
				source, err := responsesImageSource(part)
				if err != nil {
					return nil, err
				}
				if source != nil {
					parts = append(parts, map[string]any{"type": "image", "source": source})
				}
			}
		}
		return parts, nil
	default:
		return nil, nil
	}
}

func responsesImageSource(part map[string]any) (map[string]any, error) {
	url := stringValue(part["image_url"])
	if url == "" {
		return nil, nil
	}
	return dataURLToAnthropicSource(url)
}

func responsesFunctionCallToAnthropic(item map[string]any) (map[string]any, error) {
	input := map[string]any{}
	if arguments := rawStringValue(item["arguments"]); strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return nil, fmt.Errorf("invalid function_call arguments: %w", err)
		}
	}
	return map[string]any{
		"type":  "tool_use",
		"id":    responseCallID(item),
		"name":  stringValue(item["name"]),
		"input": input,
	}, nil
}

func responsesFunctionOutputToAnthropic(item map[string]any) map[string]any {
	return map[string]any{
		"role": "user",
		"content": []map[string]any{{
			"type":        "tool_result",
			"tool_use_id": responseCallID(item),
			"content":     rawStringValue(item["output"]),
		}},
	}
}

func removeAssistantPrefill(messages []map[string]any) []map[string]any {
	for len(messages) > 0 {
		role, _ := messages[len(messages)-1]["role"].(string)
		if role != "assistant" {
			return messages
		}
		messages = messages[:len(messages)-1]
	}
	return messages
}

func translateAnthropicToResponses(resp AnthropicResponse, model string) ResponsesResponse {
	output := make([]ResponsesOutputItem, 0, len(resp.Content))
	var outputText strings.Builder
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			outputText.WriteString(block.Text)
			output = append(output, ResponsesOutputItem{
				ID:     responseOutputID("msg"),
				Type:   "message",
				Status: "completed",
				Role:   "assistant",
				Content: []ResponsesContentPart{{
					Type:        "output_text",
					Text:        block.Text,
					Annotations: []any{},
				}},
			})
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			output = append(output, ResponsesOutputItem{
				ID:        block.ID,
				Type:      "function_call",
				Status:    "completed",
				CallID:    block.ID,
				Name:      block.Name,
				Arguments: string(args),
			})
		}
	}
	return ResponsesResponse{
		ID:         responseID(resp.ID),
		Object:     "response",
		CreatedAt:  time.Now().Unix(),
		Status:     "completed",
		Model:      model,
		Output:     output,
		OutputText: outputText.String(),
		Usage:      responsesUsage(resp.Usage),
	}
}

func (s *Server) streamResponses(w http.ResponseWriter, ctx context.Context, modelID string, exposedModel string, body []byte) {
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

	state := newResponsesStreamState(exposedModel)
	state.write(w, flusher, "response.created", state.responseEvent("response.created", "in_progress"))
	for chunk := range chunks {
		var ev AnthropicStreamEvent
		if err := json.Unmarshal(chunk, &ev); err != nil {
			continue
		}
		state.translate(w, flusher, ev)
	}
	if err := <-errs; err != nil {
		state.write(w, flusher, "error", map[string]any{"type": "error", "message": err.Error()})
		return
	}
	state.write(w, flusher, "response.completed", state.responseEvent("response.completed", "completed"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

type ResponsesResponse struct {
	ID         string                `json:"id"`
	Object     string                `json:"object"`
	CreatedAt  int64                 `json:"created_at"`
	Status     string                `json:"status"`
	Model      string                `json:"model"`
	Output     []ResponsesOutputItem `json:"output"`
	OutputText string                `json:"output_text,omitempty"`
	Usage      *ResponsesUsage       `json:"usage,omitempty"`
}

type ResponsesOutputItem struct {
	ID        string                 `json:"id,omitempty"`
	Type      string                 `json:"type"`
	Status    string                 `json:"status,omitempty"`
	Role      string                 `json:"role,omitempty"`
	Content   []ResponsesContentPart `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
}

type ResponsesContentPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Annotations []any  `json:"annotations,omitempty"`
}

type ResponsesUsage struct {
	InputTokens         int `json:"input_tokens"`
	InputTokensDetails  any `json:"input_tokens_details,omitempty"`
	OutputTokens        int `json:"output_tokens"`
	OutputTokensDetails any `json:"output_tokens_details,omitempty"`
	TotalTokens         int `json:"total_tokens"`
}

func responsesUsage(usage AnthropicUsage) *ResponsesUsage {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}
	return &ResponsesUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.InputTokens + usage.OutputTokens,
	}
}

func responseCallID(item map[string]any) string {
	if callID := stringValue(item["call_id"]); callID != "" {
		return callID
	}
	return stringValue(item["id"])
}

func responseOutputID(prefix string) string {
	return prefix + "_" + time.Now().Format("20060102150405")
}

func stringValue(value any) string {
	return strings.TrimSpace(rawStringValue(value))
}

func rawStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func boolValue(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}

func intValue(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
