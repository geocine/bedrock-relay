package relay

import (
	"encoding/json"
	"net/http"
	"time"
)

type responsesStreamState struct {
	id          string
	model       string
	createdAt   int64
	outputIndex int
	itemOpen    bool
	itemID      string
	itemType    string
	itemName    string
	text        string
	arguments   string
	output      []ResponsesOutputItem
	usage       AnthropicUsage
}

func newResponsesStreamState(model string) *responsesStreamState {
	return &responsesStreamState{
		id:        "resp_" + time.Now().Format("20060102150405"),
		model:     model,
		createdAt: time.Now().Unix(),
		output:    []ResponsesOutputItem{},
	}
}

func (s *responsesStreamState) translate(w http.ResponseWriter, flusher http.Flusher, ev AnthropicStreamEvent) {
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			s.id = responseID(ev.Message.ID)
			s.usage.InputTokens = ev.Message.Usage.InputTokens
		}
	case "content_block_start":
		if ev.ContentBlock == nil {
			return
		}
		switch ev.ContentBlock.Type {
		case "text":
			s.itemOpen = true
			s.itemID = responseOutputID("msg")
			s.itemType = "message"
			s.itemName = ""
			s.text = ""
			s.arguments = ""
			s.outputIndex = ev.Index
			s.write(w, flusher, "response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": s.outputIndex,
				"item": map[string]any{
					"id":      s.itemID,
					"type":    "message",
					"status":  "in_progress",
					"role":    "assistant",
					"content": []any{},
				},
			})
			s.write(w, flusher, "response.content_part.added", map[string]any{
				"type":          "response.content_part.added",
				"item_id":       s.itemID,
				"output_index":  s.outputIndex,
				"content_index": 0,
				"part":          map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
			})
		case "tool_use":
			s.itemID = ev.ContentBlock.ID
			s.itemType = "function_call"
			s.itemName = ev.ContentBlock.Name
			s.text = ""
			s.arguments = ""
			s.outputIndex = ev.Index
			s.write(w, flusher, "response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": s.outputIndex,
				"item": map[string]any{
					"id":        ev.ContentBlock.ID,
					"type":      "function_call",
					"status":    "in_progress",
					"call_id":   ev.ContentBlock.ID,
					"name":      ev.ContentBlock.Name,
					"arguments": "",
				},
			})
		}
	case "content_block_delta":
		if ev.Delta == nil {
			return
		}
		switch stringValue(ev.Delta["type"]) {
		case "text_delta":
			text := rawStringValue(ev.Delta["text"])
			s.text += text
			s.write(w, flusher, "response.output_text.delta", map[string]any{
				"type":          "response.output_text.delta",
				"item_id":       s.itemID,
				"output_index":  ev.Index,
				"content_index": 0,
				"delta":         text,
			})
		case "input_json_delta":
			partial := rawStringValue(ev.Delta["partial_json"])
			s.arguments += partial
			s.write(w, flusher, "response.function_call_arguments.delta", map[string]any{
				"type":         "response.function_call_arguments.delta",
				"item_id":      s.itemID,
				"output_index": ev.Index,
				"delta":        partial,
			})
		}
	case "content_block_stop":
		switch s.itemType {
		case "message":
			s.write(w, flusher, "response.output_text.done", map[string]any{
				"type":          "response.output_text.done",
				"item_id":       s.itemID,
				"output_index":  ev.Index,
				"content_index": 0,
				"text":          s.text,
			})
			s.write(w, flusher, "response.content_part.done", map[string]any{
				"type":          "response.content_part.done",
				"item_id":       s.itemID,
				"output_index":  ev.Index,
				"content_index": 0,
				"part":          map[string]any{"type": "output_text", "text": s.text, "annotations": []any{}},
			})
			s.itemOpen = false
			item := ResponsesOutputItem{
				ID:     s.itemID,
				Type:   "message",
				Status: "completed",
				Role:   "assistant",
				Content: []ResponsesContentPart{{
					Type:        "output_text",
					Text:        s.text,
					Annotations: []any{},
				}},
			}
			s.output = append(s.output, item)
			s.write(w, flusher, "response.output_item.done", map[string]any{
				"type":         "response.output_item.done",
				"output_index": ev.Index,
				"item":         item,
			})
		case "function_call":
			s.write(w, flusher, "response.function_call_arguments.done", map[string]any{
				"type":         "response.function_call_arguments.done",
				"item_id":      s.itemID,
				"output_index": ev.Index,
				"arguments":    s.arguments,
			})
			item := ResponsesOutputItem{
				ID:        s.itemID,
				Type:      "function_call",
				Status:    "completed",
				CallID:    s.itemID,
				Name:      s.itemName,
				Arguments: s.arguments,
			}
			s.output = append(s.output, item)
			s.write(w, flusher, "response.output_item.done", map[string]any{
				"type":         "response.output_item.done",
				"output_index": ev.Index,
				"item":         item,
			})
		}
	case "message_delta":
		if ev.Usage != nil {
			s.usage.OutputTokens = ev.Usage.OutputTokens
		}
	}
}

func (s *responsesStreamState) responseEvent(eventType string, status string) map[string]any {
	response := s.response(status)
	if status == "completed" {
		response["end_turn"] = true
	}
	return map[string]any{
		"type":     eventType,
		"response": response,
	}
}

func (s *responsesStreamState) response(status string) map[string]any {
	return map[string]any{
		"id":         s.id,
		"object":     "response",
		"created_at": s.createdAt,
		"status":     status,
		"model":      s.model,
		"output":     s.output,
		"usage":      responsesStreamUsage(s.usage),
	}
}

func responsesStreamUsage(usage AnthropicUsage) any {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}
	return map[string]any{
		"input_tokens":          usage.InputTokens,
		"input_tokens_details":  nil,
		"output_tokens":         usage.OutputTokens,
		"output_tokens_details": nil,
		"total_tokens":          usage.InputTokens + usage.OutputTokens,
	}
}

func (s *responsesStreamState) write(w http.ResponseWriter, flusher http.Flusher, event string, value any) {
	body, _ := json.Marshal(value)
	writeSSEData(w, flusher, event, body)
}
