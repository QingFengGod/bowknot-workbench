package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capturedRequest struct {
	model string
	auth  string
}

func newStreamServer(t *testing.T, reply string, capture *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		capture.model = body.Model
		capture.auth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", reply)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func collectText(t *testing.T, provider Provider, model string) string {
	t.Helper()
	chunks, err := provider.Stream(context.Background(), ChatRequest{Model: model})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	var out strings.Builder
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("chunk error = %v", chunk.Err)
		}
		out.WriteString(chunk.Delta)
	}
	return out.String()
}

func TestRouterRoutesByModelKey(t *testing.T) {
	first, second := &capturedRequest{}, &capturedRequest{}
	serverOne := newStreamServer(t, "one", first)
	defer serverOne.Close()
	serverTwo := newStreamServer(t, "two", second)
	defer serverTwo.Close()

	provider := NewRouter(RouterOptions{
		DefaultKey: "p2/m2",
		Routes: map[string]Route{
			"p1/m1": {ProviderID: "p1", BaseURL: serverOne.URL + "/v1", APIKey: "key-one", ModelID: "upstream-1"},
			"p2/m2": {ProviderID: "p2", BaseURL: serverTwo.URL + "/v1", APIKey: "key-two", ModelID: "upstream-2"},
		},
		Order: []string{"p1/m1", "p2/m2"},
		Models: []ModelInfo{
			{ID: "p1/m1", Name: "one"},
			{ID: "p2/m2", Name: "two"},
		},
	})

	if got := collectText(t, provider, "p1/m1"); got != "one" {
		t.Fatalf("p1 回复 = %q", got)
	}
	if first.model != "upstream-1" {
		t.Fatalf("p1 上游模型 = %q", first.model)
	}
	if first.auth != "Bearer key-one" {
		t.Fatalf("p1 鉴权头 = %q", first.auth)
	}

	if got := collectText(t, provider, "p2/m2"); got != "two" {
		t.Fatalf("p2 回复 = %q", got)
	}
	if second.model != "upstream-2" || second.auth != "Bearer key-two" {
		t.Fatalf("p2 请求 = %+v", second)
	}
}

func TestRouterUsesDefaultModelWhenEmpty(t *testing.T) {
	first, second := &capturedRequest{}, &capturedRequest{}
	serverOne := newStreamServer(t, "one", first)
	defer serverOne.Close()
	serverTwo := newStreamServer(t, "two", second)
	defer serverTwo.Close()

	provider := NewRouter(RouterOptions{
		DefaultKey: "p2/m2",
		Routes: map[string]Route{
			"p1/m1": {ProviderID: "p1", BaseURL: serverOne.URL + "/v1", ModelID: "upstream-1"},
			"p2/m2": {ProviderID: "p2", BaseURL: serverTwo.URL + "/v1", ModelID: "upstream-2"},
		},
		Order: []string{"p1/m1", "p2/m2"},
	})

	if got := collectText(t, provider, ""); got != "two" {
		t.Fatalf("默认模型回复 = %q", got)
	}
	if second.model != "upstream-2" {
		t.Fatalf("默认模型上游 = %q", second.model)
	}
}

func TestRouterRejectsUnknownModel(t *testing.T) {
	server := newStreamServer(t, "x", &capturedRequest{})
	defer server.Close()

	provider := NewRouter(RouterOptions{
		Routes: map[string]Route{
			"p1/m1": {ProviderID: "p1", BaseURL: server.URL + "/v1", ModelID: "upstream-1"},
		},
		Order: []string{"p1/m1"},
	})

	_, err := provider.Stream(context.Background(), ChatRequest{Model: "p1/unknown"})
	if err == nil || !strings.Contains(err.Error(), "未配置或已停用") {
		t.Fatalf("未知模型错误 = %v", err)
	}
}

func TestRouterModelsReturnsConfiguredList(t *testing.T) {
	server := newStreamServer(t, "x", &capturedRequest{})
	defer server.Close()

	provider := NewRouter(RouterOptions{
		Routes: map[string]Route{
			"p1/m1": {ProviderID: "p1", BaseURL: server.URL + "/v1", ModelID: "upstream-1"},
		},
		Order:  []string{"p1/m1"},
		Models: []ModelInfo{{ID: "p1/m1", Name: "模型一"}},
	})

	models, err := provider.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "p1/m1" || models[0].Name != "模型一" {
		t.Fatalf("Models() = %+v", models)
	}
}

func TestNewRouterWithoutRoutesFallsBackToFake(t *testing.T) {
	provider := NewRouter(RouterOptions{})
	chunks, err := provider.Stream(context.Background(), ChatRequest{Model: "anything"})
	if err != nil {
		t.Fatalf("Fake Stream() error = %v", err)
	}
	for range chunks {
	}
}
