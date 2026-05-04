package relay

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAliases(t *testing.T) {
	server := NewServer(AppConfig{}, nil)
	for _, path := range []string{"/health", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		server.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned status %d", path, rec.Code)
		}
	}
}

func TestChatCompletionsDoesNotRequireAuthorizationHeader(t *testing.T) {
	catalog := ModelCatalog{Models: []ModelEntry{{Alias: "sonnet", ID: "bedrock-sonnet"}}}
	if err := catalog.index(); err != nil {
		t.Fatal(err)
	}
	server := NewServer(AppConfig{Models: catalog}, fakeBedrockInvoker{
		response: []byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model": "sonnet",
		"messages": [{"role": "user", "content": "hello"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected request without authorization header to succeed, got status %d with body %s", rec.Code, rec.Body.String())
	}
}

type fakeBedrockInvoker struct {
	response []byte
}

func (f fakeBedrockInvoker) Invoke(context.Context, string, []byte) ([]byte, error) {
	return f.response, nil
}

func (f fakeBedrockInvoker) Stream(context.Context, string, []byte) (<-chan []byte, <-chan error, error) {
	chunks := make(chan []byte)
	errs := make(chan error, 1)
	close(chunks)
	close(errs)
	return chunks, errs, nil
}
