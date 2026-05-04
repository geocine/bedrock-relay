package relay

import (
	"encoding/json"
	"strings"
)

func normalizeResponsesRequestForLlamaCpp(bodyBytes []byte) ([]byte, bool, error) {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return nil, false, err
	}

	changed := false
	if normalizeResponsesToolsForLlamaCpp(body) {
		changed = true
	}
	if hoistResponsesInstructionMessagesForLlamaCpp(body) {
		changed = true
	}

	if !changed {
		return bodyBytes, false, nil
	}

	normalizedBody, err := json.Marshal(body)
	if err != nil {
		return nil, false, err
	}
	return normalizedBody, true, nil
}

func normalizeResponsesToolsForLlamaCpp(body map[string]any) bool {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return false
	}

	normalizedTools := make([]any, 0, len(tools))
	changed := false
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			normalizedTools = append(normalizedTools, rawTool)
			continue
		}

		switch toolType(tool) {
		case "function":
			normalizedTools = append(normalizedTools, tool)
		case "custom":
			normalizedTools = append(normalizedTools, customResponsesToolToFunction(tool))
			changed = true
		case "apply_patch":
			normalizedTools = append(normalizedTools, customResponsesToolToFunction(tool))
			changed = true
		case "local_shell", "shell":
			normalizedTools = append(normalizedTools, shellResponsesToolToFunction(tool))
			changed = true
		case "namespace":
			nestedTools, flattened := flattenResponsesNamespaceTools(tool)
			normalizedTools = append(normalizedTools, nestedTools...)
			changed = changed || flattened
		default:
			changed = true
		}
	}

	if !changed {
		return false
	}

	body["tools"] = normalizedTools
	normalizeResponsesToolChoice(body)
	return true
}

func hoistResponsesInstructionMessagesForLlamaCpp(body map[string]any) bool {
	input, ok := body["input"].([]any)
	if !ok || len(input) == 0 {
		return false
	}

	keptInput := make([]any, 0, len(input))
	instructionSections := make([]string, 0)
	changed := false
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok || toolType(item) != "message" {
			keptInput = append(keptInput, rawItem)
			continue
		}

		role := strings.ToLower(strings.TrimSpace(stringValue(item["role"])))
		if role != "system" && role != "developer" {
			keptInput = append(keptInput, rawItem)
			continue
		}

		if text := responsesMessageText(item); text != "" {
			instructionSections = append(instructionSections, text)
		}
		changed = true
	}

	if !changed {
		return false
	}

	body["input"] = keptInput
	if len(instructionSections) > 0 {
		body["instructions"] = joinResponsesInstructions(stringValue(body["instructions"]), instructionSections)
	}
	return true
}

func responsesMessageText(message map[string]any) string {
	switch content := message["content"].(type) {
	case string:
		return strings.TrimSpace(content)
	case []any:
		parts := make([]string, 0, len(content))
		for _, rawContentItem := range content {
			contentItem, ok := rawContentItem.(map[string]any)
			if !ok {
				continue
			}
			contentType := toolType(contentItem)
			if contentType != "input_text" && contentType != "output_text" && contentType != "text" {
				continue
			}
			if text := stringValue(contentItem["text"]); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func joinResponsesInstructions(existing string, sections []string) string {
	allSections := make([]string, 0, len(sections)+1)
	if existing = strings.TrimSpace(existing); existing != "" {
		allSections = append(allSections, existing)
	}
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			allSections = append(allSections, section)
		}
	}
	return strings.Join(allSections, "\n\n")
}

func toolType(tool map[string]any) string {
	return stringValue(tool["type"])
}

func customResponsesToolToFunction(tool map[string]any) map[string]any {
	name := responseToolName(tool, "custom_tool")
	description := responseToolDescription(tool)
	if description == "" {
		description = "Freeform tool input. Put the entire tool input in the input field."
	} else {
		description += "\n\nThis upstream only accepts function tools. Put the complete freeform tool input in the input field."
	}

	return map[string]any{
		"type":        "function",
		"name":        name,
		"description": description,
		"strict":      false,
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "The complete freeform input for this tool.",
				},
			},
			"required":             []any{"input"},
			"additionalProperties": false,
		},
	}
}

func flattenResponsesNamespaceTools(namespace map[string]any) ([]any, bool) {
	nestedTools, ok := namespace["tools"].([]any)
	if !ok || len(nestedTools) == 0 {
		return nil, true
	}

	flattened := make([]any, 0, len(nestedTools))
	for _, rawNestedTool := range nestedTools {
		nestedTool, ok := rawNestedTool.(map[string]any)
		if !ok {
			continue
		}

		switch toolType(nestedTool) {
		case "function":
			flattened = append(flattened, nestedTool)
		case "custom":
			flattened = append(flattened, customResponsesToolToFunction(nestedTool))
		case "apply_patch":
			flattened = append(flattened, customResponsesToolToFunction(nestedTool))
		case "local_shell", "shell":
			flattened = append(flattened, shellResponsesToolToFunction(nestedTool))
		}
	}

	return flattened, true
}

func shellResponsesToolToFunction(tool map[string]any) map[string]any {
	name := responseToolName(tool, toolType(tool))
	if name == "" {
		name = "shell"
	}
	description := responseToolDescription(tool)
	if description == "" {
		description = "Runs a shell command and returns its output."
	}

	return map[string]any{
		"type":        "function",
		"name":        name,
		"description": description,
		"strict":      false,
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "array",
					"description": "The command to execute.",
					"items": map[string]any{
						"type": "string",
					},
				},
				"workdir": map[string]any{
					"type":        "string",
					"description": "The working directory to execute the command in.",
				},
				"timeout_ms": map[string]any{
					"type":        "number",
					"description": "The timeout for the command in milliseconds.",
				},
			},
			"required":             []any{"command"},
			"additionalProperties": false,
		},
	}
}

func responseToolName(tool map[string]any, fallback string) string {
	if name := stringValue(tool["name"]); name != "" {
		return name
	}
	return strings.TrimSpace(fallback)
}

func responseToolDescription(tool map[string]any) string {
	return stringValue(tool["description"])
}

func normalizeResponsesToolChoice(body map[string]any) {
	toolChoice, ok := body["tool_choice"].(map[string]any)
	if !ok {
		return
	}

	switch toolType(toolChoice) {
	case "custom":
		toolChoice["type"] = "function"
	case "apply_patch", "shell":
		if stringValue(toolChoice["name"]) == "" {
			toolChoice["name"] = toolType(toolChoice)
		}
		toolChoice["type"] = "function"
	}
}
