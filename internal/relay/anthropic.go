package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req AnthropicMessagesRequest
	var raw map[string]any
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&raw); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	data, _ := json.Marshal(raw)
	if err := json.Unmarshal(data, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	bedrockModel, exposedModel, err := modelFromRequest(req.Model, s.config.Models)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := anthropicBedrockBody(raw, r.Header.Get("anthropic-beta"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Stream {
		s.streamAnthropic(w, r.Context(), bedrockModel, body)
		return
	}

	resp, err := invokeAnthropic(r.Context(), s.bedrock, bedrockModel, body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if resp.Model == "" {
		resp.Model = exposedModel
	}
	writeJSON(w, http.StatusOK, resp)
}

func anthropicBedrockBody(raw map[string]any, betaHeader string) ([]byte, error) {
	delete(raw, "model")
	delete(raw, "stream")
	delete(raw, "metadata")
	raw["anthropic_version"] = anthropicVersion
	if _, ok := raw["max_tokens"]; !ok {
		raw["max_tokens"] = defaultMaxTokens
	}
	betas := parseBodyBetas(raw["anthropic_beta"])
	for _, beta := range parseAnthropicBeta(betaHeader) {
		betas = appendUnique(betas, beta)
	}
	if err := applyEffortLevel(raw, &betas); err != nil {
		return nil, err
	}
	if len(betas) > 0 {
		raw["anthropic_beta"] = betas
	}
	return json.Marshal(raw)
}

func parseBodyBetas(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			out = appendUnique(out, strings.TrimSpace(text))
		}
	}
	return out
}

func parseAnthropicBeta(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (s *Server) streamAnthropic(w http.ResponseWriter, ctx context.Context, modelID string, body []byte) {
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

	for chunk := range chunks {
		var ev AnthropicStreamEvent
		if err := json.Unmarshal(chunk, &ev); err != nil || ev.Type == "" {
			writeSSEData(w, flusher, "", chunk)
			continue
		}
		writeSSEData(w, flusher, ev.Type, chunk)
	}
	if err := <-errs; err != nil {
		event, _ := json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]string{
				"type":    "api_error",
				"message": err.Error(),
			},
		})
		writeSSEData(w, flusher, "error", event)
	}
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req AnthropicMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"input_tokens": approximateAnthropicTokens(req)})
}

func approximateAnthropicTokens(req AnthropicMessagesRequest) int {
	chars := len(req.System)
	for _, msg := range req.Messages {
		chars += len(msg.Content)
	}
	if chars == 0 {
		return 0
	}
	return (chars + 3) / 4
}
