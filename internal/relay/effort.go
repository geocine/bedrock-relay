package relay

import (
	"fmt"
	"strings"
)

const effortBeta = "effort-2025-11-24"

var supportedEffortLevels = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"xhigh":  true,
	"max":    true,
}

func normalizeEffortLevel(raw string) (string, error) {
	effort := strings.ToLower(strings.TrimSpace(raw))
	if effort == "" {
		return "", nil
	}
	if !supportedEffortLevels[effort] {
		return "", fmt.Errorf("unsupported effortLevel %q (use low, medium, high, xhigh, or max)", raw)
	}
	return effort, nil
}

func normalizeResponsesEffortLevel(raw string) (string, error) {
	effort := strings.ToLower(strings.TrimSpace(raw))
	if effort == "" {
		return "", nil
	}
	switch effort {
	case "minimal":
		return "low", nil
	case "low":
		return "medium", nil
	case "medium":
		return "high", nil
	case "high":
		return "xhigh", nil
	case "xhigh":
		return "max", nil
	case "max":
		return "max", nil
	default:
		return "", fmt.Errorf("unsupported reasoning.effort %q (use minimal, low, medium, high, xhigh, or max)", raw)
	}
}

func applyEffortLevel(raw map[string]any, betas *[]string) error {
	effort, err := extractEffortLevel(raw)
	if err != nil {
		return err
	}
	if effort == "" {
		return nil
	}

	outputConfig, _ := raw["output_config"].(map[string]any)
	if outputConfig == nil {
		outputConfig = map[string]any{}
	}
	outputConfig["effort"] = effort
	raw["output_config"] = outputConfig
	*betas = appendUnique(*betas, effortBeta)
	return nil
}

func extractEffortLevel(raw map[string]any) (string, error) {
	keys := []string{"effortLevel", "effort_level", "reasoning_effort", "effort"}
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			delete(raw, key)
			effort, err := valueAsEffort(value)
			if err != nil || effort != "" {
				return effort, err
			}
		}
	}

	if reasoning, ok := raw["reasoning"].(map[string]any); ok {
		delete(raw, "reasoning")
		if value, ok := reasoning["effort"]; ok {
			return valueAsEffort(value)
		}
	}

	if outputConfig, ok := raw["output_config"].(map[string]any); ok {
		if value, ok := outputConfig["effort"]; ok {
			return valueAsEffort(value)
		}
	}
	return "", nil
}

func valueAsEffort(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("effortLevel must be a string")
	}
	return normalizeEffortLevel(text)
}

func appendUnique(items []string, next string) []string {
	for _, item := range items {
		if item == next {
			return items
		}
	}
	return append(items, next)
}
