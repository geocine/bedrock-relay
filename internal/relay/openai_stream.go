package relay

import (
	"encoding/json"
	"fmt"
	"time"
)

type openAIStreamState struct {
	id        string
	model     string
	created   int64
	toolIndex map[int]int
	usage     AnthropicUsage
}

func newOpenAIStreamState(model string) *openAIStreamState {
	return &openAIStreamState{
		id:        "chatcmpl-" + time.Now().Format("20060102150405"),
		model:     model,
		created:   time.Now().Unix(),
		toolIndex: map[int]int{},
	}
}

func (s *openAIStreamState) roleChunk() OpenAIChatResponse {
	return s.chunk(&OpenAIMessage{Role: "assistant"}, nil, nil)
}

func (s *openAIStreamState) translate(ev AnthropicStreamEvent) []OpenAIChatResponse {
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			s.id = responseID(ev.Message.ID)
			s.usage.InputTokens = ev.Message.Usage.InputTokens
		}
	case "content_block_start":
		if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
			idx := ev.Index
			s.toolIndex[idx] = idx
			callIdx := idx
			return []OpenAIChatResponse{s.chunk(&OpenAIMessage{
				ToolCalls: []OpenAIToolCall{{
					Index: &callIdx,
					ID:    ev.ContentBlock.ID,
					Type:  "function",
					Function: OpenAIFunctionCall{
						Name: ev.ContentBlock.Name,
					},
				}},
			}, nil, nil)}
		}
	case "content_block_delta":
		if ev.Delta == nil {
			return nil
		}
		deltaType, _ := ev.Delta["type"].(string)
		switch deltaType {
		case "text_delta":
			text, _ := ev.Delta["text"].(string)
			raw, _ := json.Marshal(text)
			return []OpenAIChatResponse{s.chunk(&OpenAIMessage{Content: raw}, nil, nil)}
		case "input_json_delta":
			partial, _ := ev.Delta["partial_json"].(string)
			idx := ev.Index
			return []OpenAIChatResponse{s.chunk(&OpenAIMessage{
				ToolCalls: []OpenAIToolCall{{
					Index: &idx,
					Function: OpenAIFunctionCall{
						Arguments: partial,
					},
				}},
			}, nil, nil)}
		}
	case "message_delta":
		if ev.Usage != nil {
			s.usage.OutputTokens = ev.Usage.OutputTokens
		}
		reason := "stop"
		if ev.Delta != nil {
			if raw, ok := ev.Delta["stop_reason"].(string); ok {
				reason = mapAnthropicStopToOpenAI(raw)
			}
		}
		usage := openAIUsage(s.usage)
		return []OpenAIChatResponse{s.chunk(&OpenAIMessage{}, &reason, usage)}
	}
	return nil
}

func (s *openAIStreamState) chunk(delta *OpenAIMessage, finish *string, usage *OpenAIUsage) OpenAIChatResponse {
	return OpenAIChatResponse{
		ID:      s.id,
		Object:  "chat.completion.chunk",
		Created: s.created,
		Model:   s.model,
		Choices: []OpenAIChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finish,
		}},
		Usage: usage,
	}
}

func (s *openAIStreamState) String() string {
	return fmt.Sprintf("%s/%s", s.id, s.model)
}
