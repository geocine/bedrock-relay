package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesNormalizeCustomToolsBecomeFunctions(t *testing.T) {
	body := []byte(`{
		"model": "sonnet",
		"tools": [
			{
				"type": "custom",
				"name": "apply_patch",
				"description": "Use apply_patch",
				"format": {"type": "grammar", "syntax": "lark", "definition": "start: /.+/"}
			}
		],
		"tool_choice": {"type": "custom", "name": "apply_patch"}
	}`)

	normalized, changed, err := normalizeResponsesRequestForLlamaCpp(body)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected request to be normalized")
	}

	var decoded map[string]any
	if err := json.Unmarshal(normalized, &decoded); err != nil {
		t.Fatal(err)
	}
	tools := decoded["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "apply_patch" || tool["strict"] != false {
		t.Fatalf("unexpected normalized tool: %#v", tool)
	}
	parameters := tool["parameters"].(map[string]any)
	if parameters["additionalProperties"] != false {
		t.Fatalf("expected additionalProperties=false: %#v", parameters)
	}
	toolChoice := decoded["tool_choice"].(map[string]any)
	if toolChoice["type"] != "function" || toolChoice["name"] != "apply_patch" {
		t.Fatalf("unexpected normalized tool choice: %#v", toolChoice)
	}
}

func TestResponsesNormalizeHoistsSystemAndDeveloperMessages(t *testing.T) {
	body := []byte(`{
		"model": "sonnet",
		"instructions": "Base instructions.",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]},
			{"type": "message", "role": "developer", "content": [{"type": "input_text", "text": "Developer instructions."}]},
			{"type": "message", "role": "system", "content": "System update."}
		]
	}`)

	normalized, changed, err := normalizeResponsesRequestForLlamaCpp(body)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected request to be normalized")
	}

	var decoded map[string]any
	if err := json.Unmarshal(normalized, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["instructions"] != "Base instructions.\n\nDeveloper instructions.\n\nSystem update." {
		t.Fatalf("unexpected instructions: %#v", decoded["instructions"])
	}
	input := decoded["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("expected one remaining input item, got %d", len(input))
	}
}

func TestResponsesEndpointNormalizesForBedrock(t *testing.T) {
	catalog := ModelCatalog{Models: []ModelEntry{{Alias: "sonnet", ID: "bedrock-sonnet"}}}
	if err := catalog.index(); err != nil {
		t.Fatal(err)
	}
	bedrock := &captureBedrockInvoker{
		response: []byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":2,"output_tokens":1}}`),
	}
	server := NewServer(AppConfig{Models: catalog}, bedrock)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{
		"model": "sonnet",
		"instructions": "Base",
		"tools": [{"type": "custom", "name": "apply_patch"}],
		"input": [
			{"type": "message", "role": "developer", "content": "Dev"},
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]}
		]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	var sent map[string]any
	if err := json.Unmarshal(bedrock.body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["system"] != "Base\n\nDev" {
		t.Fatalf("unexpected Bedrock system: %#v", sent["system"])
	}
	tools := sent["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["name"] != "apply_patch" {
		t.Fatalf("unexpected Bedrock tool: %#v", tool)
	}
	messages := sent["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected one Bedrock message, got %d", len(messages))
	}
}

func TestResponsesTranslationDropsTrailingAssistantPrefill(t *testing.T) {
	req := map[string]any{
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "hello"},
			map[string]any{"type": "message", "role": "assistant", "content": "hi"},
		},
	}

	translated, err := translateResponsesToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	messages := translated["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("expected trailing assistant prefill to be removed, got %#v", messages)
	}
	if messages[0]["role"] != "user" {
		t.Fatalf("expected remaining message to be user, got %#v", messages[0])
	}
}

func TestResponsesEffortMapsCodexScaleToBedrockScale(t *testing.T) {
	req := map[string]any{
		"reasoning": map[string]any{"effort": "xhigh"},
		"input":     []any{map[string]any{"type": "message", "role": "user", "content": "hello"}},
	}

	translated, err := translateResponsesToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	outputConfig := translated["output_config"].(map[string]any)
	if outputConfig["effort"] != "max" {
		t.Fatalf("unexpected Bedrock effort: %#v", outputConfig)
	}
}

func TestResponsesStreamTextDeltaPreservesWhitespace(t *testing.T) {
	st := newResponsesStreamState("sonnet")
	rec := httptest.NewRecorder()
	flusher := flushRecorder{ResponseRecorder: rec}

	st.translate(rec, flusher, AnthropicStreamEvent{
		Type:         "content_block_start",
		Index:        0,
		ContentBlock: &AnthropicContentBlock{Type: "text"},
	})
	st.translate(rec, flusher, AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: 0,
		Delta: map[string]any{"type": "text_delta", "text": " How"},
	})
	st.translate(rec, flusher, AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: 0,
		Delta: map[string]any{"type": "text_delta", "text": " can"},
	})
	st.translate(rec, flusher, AnthropicStreamEvent{Type: "content_block_stop", Index: 0})

	body := rec.Body.String()
	if !strings.Contains(body, `"delta":" How"`) || !strings.Contains(body, `"delta":" can"`) {
		t.Fatalf("stream deltas did not preserve leading spaces: %s", body)
	}
	if !strings.Contains(body, `"text":" How can"`) {
		t.Fatalf("completed text did not preserve spaces: %s", body)
	}
}

func TestResponsesStreamCompletedUsesCodexEnvelope(t *testing.T) {
	st := newResponsesStreamState("sonnet")
	st.id = "resp_1"
	st.usage = AnthropicUsage{InputTokens: 3, OutputTokens: 4}

	event := st.responseEvent("response.completed", "completed")
	if event["type"] != "response.completed" {
		t.Fatalf("unexpected event type: %#v", event)
	}
	response := event["response"].(map[string]any)
	if response["id"] != "resp_1" || response["end_turn"] != true {
		t.Fatalf("unexpected response envelope: %#v", response)
	}
	usage := response["usage"].(map[string]any)
	if _, ok := usage["input_tokens_details"]; !ok {
		t.Fatalf("missing input_tokens_details: %#v", usage)
	}
	if _, ok := usage["output_tokens_details"]; !ok {
		t.Fatalf("missing output_tokens_details: %#v", usage)
	}
	if usage["total_tokens"] != 7 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

func TestResponsesStreamToolCallCompletesAsFunctionCall(t *testing.T) {
	st := newResponsesStreamState("sonnet")
	rec := httptest.NewRecorder()
	flusher := flushRecorder{ResponseRecorder: rec}

	st.translate(rec, flusher, AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: 0,
		ContentBlock: &AnthropicContentBlock{
			Type: "tool_use",
			ID:   "toolu_1",
			Name: "apply_patch",
		},
	})
	st.translate(rec, flusher, AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: 0,
		Delta: map[string]any{"type": "input_json_delta", "partial_json": `{"input":"x"}`},
	})
	st.translate(rec, flusher, AnthropicStreamEvent{Type: "content_block_stop", Index: 0})

	body := rec.Body.String()
	if !strings.Contains(body, "response.function_call_arguments.done") {
		t.Fatalf("missing function_call_arguments.done event: %s", body)
	}
	if !strings.Contains(body, `"type":"function_call"`) {
		t.Fatalf("completed output item was not a function_call: %s", body)
	}
	if !strings.Contains(body, `"arguments":"{\"input\":\"x\"}"`) {
		t.Fatalf("completed output item did not include arguments: %s", body)
	}
}

type captureBedrockInvoker struct {
	body     []byte
	response []byte
}

func (f *captureBedrockInvoker) Invoke(_ context.Context, _ string, body []byte) ([]byte, error) {
	f.body = append([]byte(nil), body...)
	return f.response, nil
}

func (f *captureBedrockInvoker) Stream(context.Context, string, []byte) (<-chan []byte, <-chan error, error) {
	chunks := make(chan []byte)
	errs := make(chan error, 1)
	close(chunks)
	close(errs)
	return chunks, errs, nil
}

type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (flushRecorder) Flush() {}
