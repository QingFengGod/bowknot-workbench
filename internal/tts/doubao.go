package tts

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DoubaoOptions 是豆包语音合成 2.0（seed-tts-2.0）的连接参数。
type DoubaoOptions struct {
	BaseURL    string
	APIKey     string
	ResourceID string
	Speaker    string
	Format     string
	SampleRate int
	SpeechRate int
}

// Doubao 通过 HTTP Chunked 接口合成语音，返回完整音频。
type Doubao struct {
	opts   DoubaoOptions
	client *http.Client
}

// NewDoubao 创建豆包语音合成客户端；整体超时由调用方的 ctx 控制。
func NewDoubao(opts DoubaoOptions) *Doubao {
	return &Doubao{opts: opts, client: &http.Client{}}
}

func (d *Doubao) Name() string { return "doubao" }

// Synthesize 一次性提交文本并聚合音频分片。
func (d *Doubao) Synthesize(ctx context.Context, req Request) (Result, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Result{}, errors.New("合成文本为空")
	}
	if d.opts.APIKey == "" {
		return Result{}, errors.New("未配置语音合成 API Key")
	}
	speaker := strings.TrimSpace(req.Voice)
	if speaker == "" {
		speaker = d.opts.Speaker
	}

	payload := map[string]any{
		"req_params": map[string]any{
			"text":    text,
			"speaker": speaker,
			"audio_params": map[string]any{
				"format":      d.opts.Format,
				"sample_rate": d.opts.SampleRate,
				"speech_rate": d.opts.SpeechRate + req.Rate,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("构造合成请求失败：%w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.opts.BaseURL, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("构造合成请求失败：%w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Api-Key", d.opts.APIKey)
	httpReq.Header.Set("X-Api-Resource-Id", d.opts.ResourceID)
	httpReq.Header.Set("X-Api-Request-Id", newRequestID())

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("请求语音合成失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return Result{}, fmt.Errorf("语音合成服务返回 %d：%s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	audio, err := readAudioChunks(resp.Body)
	if err != nil {
		return Result{}, err
	}
	return Result{Audio: audio, MIME: mimeOf(d.opts.Format), SampleRate: d.opts.SampleRate}, nil
}

// readAudioChunks 逐行解析响应：code 0 携带 base64 音频，20000000 表示结束。
func readAudioChunks(r io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)

	var audio []byte
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    string `json:"data"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			return nil, fmt.Errorf("解析语音合成响应失败：%w", err)
		}
		switch {
		case chunk.Code == 20000000:
			return audio, nil
		case chunk.Code != 0:
			return nil, fmt.Errorf("语音合成失败（%d）：%s", chunk.Code, chunk.Message)
		case chunk.Data != "":
			part, err := base64.StdEncoding.DecodeString(chunk.Data)
			if err != nil {
				return nil, fmt.Errorf("解码音频分片失败：%w", err)
			}
			audio = append(audio, part...)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取语音合成响应失败：%w", err)
	}
	if len(audio) == 0 {
		return nil, errors.New("语音合成未返回音频")
	}
	return audio, nil
}

func mimeOf(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "pcm":
		return "audio/L16"
	case "ogg_opus":
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
}

// newRequestID 生成 UUID v4 作为 X-Api-Request-Id。
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", b)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
