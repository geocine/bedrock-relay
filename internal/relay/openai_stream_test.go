package relay

import "testing"

func TestOpenAIStreamTextDeltaAndUsage(t *testing.T) {
	st := newOpenAIStreamState("sonnet")
	chunks := st.translate(AnthropicStreamEvent{
		Type:    "message_start",
		Message: &AnthropicResponse{ID: "msg_1", Usage: AnthropicUsage{InputTokens: 2}},
	})
	if len(chunks) != 0 {
		t.Fatalf("message_start should not emit chunks")
	}

	chunks = st.translate(AnthropicStreamEvent{
		Type:  "content_block_delta",
		Delta: map[string]any{"type": "text_delta", "text": "hi"},
	})
	if len(chunks) != 1 || string(chunks[0].Choices[0].Delta.Content) != `"hi"` {
		t.Fatalf("unexpected text delta: %#v", chunks)
	}

	chunks = st.translate(AnthropicStreamEvent{
		Type:  "message_delta",
		Delta: map[string]any{"stop_reason": "end_turn"},
		Usage: &AnthropicUsage{OutputTokens: 4},
	})
	if len(chunks) != 1 || chunks[0].Usage.TotalTokens != 6 {
		t.Fatalf("unexpected final usage: %#v", chunks)
	}
}
