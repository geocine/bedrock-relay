package relay

import (
	"encoding/json"
	"testing"
)

func TestAnthropicBedrockBodyEffortLevel(t *testing.T) {
	raw := map[string]any{
		"model":       "claude-sonnet-4-6",
		"max_tokens":  100,
		"effortLevel": "MAX",
		"messages":    []any{map[string]any{"role": "user", "content": "hello"}},
	}
	body, err := anthropicBedrockBody(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if _, exists := out["effortLevel"]; exists {
		t.Fatal("compat effortLevel leaked into Bedrock body")
	}
	outputConfig := out["output_config"].(map[string]any)
	if outputConfig["effort"] != "max" {
		t.Fatalf("unexpected effort: %#v", outputConfig)
	}
	betas := out["anthropic_beta"].([]any)
	if len(betas) != 1 || betas[0] != effortBeta {
		t.Fatalf("unexpected betas: %#v", betas)
	}
}

func TestNormalizeEffortRejectsUnsupportedValue(t *testing.T) {
	if _, err := normalizeEffortLevel("extreme"); err == nil {
		t.Fatal("expected unsupported effort to fail")
	}
}

func TestNormalizeResponsesEffortMapsCodexScaleToBedrockScale(t *testing.T) {
	tests := map[string]string{
		"minimal": "low",
		"low":     "medium",
		"medium":  "high",
		"high":    "xhigh",
		"xhigh":   "max",
		"max":     "max",
	}

	for input, expected := range tests {
		got, err := normalizeResponsesEffortLevel(input)
		if err != nil {
			t.Fatalf("normalizeResponsesEffortLevel(%q) failed: %v", input, err)
		}
		if got != expected {
			t.Fatalf("normalizeResponsesEffortLevel(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestNormalizeEffortDoesNotAcceptCodexMinimalOutsideResponsesAPI(t *testing.T) {
	if _, err := normalizeEffortLevel("minimal"); err == nil {
		t.Fatal("expected minimal to be rejected outside Responses API")
	}
}
