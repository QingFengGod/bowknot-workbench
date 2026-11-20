package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestDoubao(t *testing.T, handler http.HandlerFunc) (*Doubao, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewDoubao(DoubaoOptions{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		ResourceID: "seed-tts-2.0",
		Speaker:    "zh_female_vv_uranus_bigtts",
		Format:     "pcm",
		SampleRate: 24000,
	}), server
}

func TestDoubaoSynthesizeAggregatesChunks(t *testing.T) {
	var gotAuth, gotResource string
	client, _ := newTestDoubao(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Api-Key")
		gotResource = r.Header.Get("X-Api-Resource-Id")

		var payload struct {
			ReqParams struct {
				Text    string `json:"text"`
				Speaker string `json:"speaker"`
			} `json:"req_params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("请求体不是合法 JSON：%v", err)
		}
		if payload.ReqParams.Text != "你好" {
			t.Errorf("text = %q, want 你好", payload.ReqParams.Text)
		}

		chunks := []map[string]any{
			{"code": 0, "message": "OK", "data": base64.StdEncoding.EncodeToString([]byte("ab"))},
			{"code": 0, "message": "OK", "data": base64.StdEncoding.EncodeToString([]byte("cd"))},
			{"code": 20000000, "message": "OK", "data": ""},
		}
		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			w.Write(append(data, '\n'))
		}
	})

	res, err := client.Synthesize(context.Background(), Request{Text: "你好"})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if string(res.Audio) != "abcd" {
		t.Fatalf("Audio = %q, want abcd", res.Audio)
	}
	if res.MIME != "audio/L16" {
		t.Fatalf("MIME = %q, want audio/L16", res.MIME)
	}
	if res.SampleRate != 24000 {
		t.Fatalf("SampleRate = %d, want 24000", res.SampleRate)
	}
	if gotAuth != "test-key" || gotResource != "seed-tts-2.0" {
		t.Fatalf("请求头错误：X-Api-Key=%q X-Api-Resource-Id=%q", gotAuth, gotResource)
	}
}

func TestDoubaoSynthesizeErrorCode(t *testing.T) {
	client, _ := newTestDoubao(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code": 45000001, "message": "invalid request"}` + "\n"))
	})

	if _, err := client.Synthesize(context.Background(), Request{Text: "你好"}); err == nil {
		t.Fatal("期望返回错误，实际成功")
	}
}

func TestDoubaoSynthesizeHTTPError(t *testing.T) {
	client, _ := newTestDoubao(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})

	if _, err := client.Synthesize(context.Background(), Request{Text: "你好"}); err == nil {
		t.Fatal("期望返回错误，实际成功")
	}
}

func TestDoubaoSynthesizeWithoutKey(t *testing.T) {
	client := NewDoubao(DoubaoOptions{BaseURL: "https://example.com"})
	if _, err := client.Synthesize(context.Background(), Request{Text: "你好"}); err == nil {
		t.Fatal("未配置 API Key 时应返回错误")
	}
}
