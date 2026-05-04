package relay

import (
	"encoding/json"
	"testing"
)

func TestTranslateOpenAIToAnthropicTextAndTools(t *testing.T) {
	req := OpenAIChatRequest{
		Model: "sonnet",
		Messages: []OpenAIMessage{
			{Role: "system", Content: rawJSON(`"be terse"`)},
			{Role: "user", Content: rawJSON(`"hello"`)},
			{
				Role: "assistant",
				ToolCalls: []OpenAIToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: OpenAIFunctionCall{
						Name:      "lookup",
						Arguments: `{"q":"x"}`,
					},
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: rawJSON(`"result"`)},
		},
		Tools: []OpenAITool{{
			Type: "function",
			Function: OpenAIFunction{
				Name:       "lookup",
				Parameters: map[string]any{"type": "object"},
			},
		}},
	}

	out, err := translateOpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if out["anthropic_version"] != anthropicVersion {
		t.Fatalf("missing anthropic version: %#v", out["anthropic_version"])
	}
	if out["system"] != "be terse" {
		t.Fatalf("unexpected system: %#v", out["system"])
	}
	messages := out["messages"].([]map[string]any)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	assistant := messages[1]["content"].([]map[string]any)
	if assistant[0]["type"] != "tool_use" || assistant[0]["name"] != "lookup" {
		t.Fatalf("unexpected assistant tool content: %#v", assistant)
	}
}

func TestTranslateAnthropicToOpenAI(t *testing.T) {
	resp := AnthropicResponse{
		ID:         "msg_1",
		StopReason: "tool_use",
		Content: []AnthropicContentBlock{
			{Type: "text", Text: "use this"},
			{Type: "tool_use", ID: "toolu_1", Name: "lookup", Input: map[string]any{"q": "x"}},
		},
		Usage: AnthropicUsage{InputTokens: 3, OutputTokens: 5},
	}

	out := translateAnthropicToOpenAI(resp, "sonnet")
	if out.ID != "msg_1" || out.Model != "sonnet" {
		t.Fatalf("unexpected response metadata: %#v", out)
	}
	if *out.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("unexpected finish reason: %s", *out.Choices[0].FinishReason)
	}
	if out.Usage.TotalTokens != 8 {
		t.Fatalf("unexpected usage: %#v", out.Usage)
	}
	if len(out.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("missing tool call: %#v", out.Choices[0].Message)
	}
}

func TestOpenAIToolChoiceToAnthropic(t *testing.T) {
	choice, err := openAIToolChoiceToAnthropic("required")
	if err != nil {
		t.Fatal(err)
	}
	if choice.(map[string]any)["type"] != "any" {
		t.Fatalf("required did not map to any: %#v", choice)
	}

	choice, err = openAIToolChoiceToAnthropic(map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "lookup",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mapped := choice.(map[string]any)
	if mapped["type"] != "tool" || mapped["name"] != "lookup" {
		t.Fatalf("unexpected function tool choice mapping: %#v", mapped)
	}
}

func TestTranslateOpenAIReasoningEffort(t *testing.T) {
	req := OpenAIChatRequest{
		Model:           "sonnet",
		ReasoningEffort: "xhigh",
		Messages:        []OpenAIMessage{{Role: "user", Content: rawJSON(`"hello"`)}},
	}
	out, err := translateOpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	outputConfig := out["output_config"].(map[string]any)
	if outputConfig["effort"] != "xhigh" {
		t.Fatalf("unexpected effort: %#v", outputConfig)
	}
	betas := out["anthropic_beta"].([]string)
	if len(betas) != 1 || betas[0] != effortBeta {
		t.Fatalf("unexpected betas: %#v", betas)
	}
}

func rawJSON(s string) json.RawMessage {
	return json.RawMessage(s)
}
