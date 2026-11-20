// Package tts 提供语音合成与本地播放。
package tts

import "context"

// Request 是一次语音合成请求。
type Request struct {
	Text  string
	Voice string
	// Rate 是语速百分比偏移，0 表示正常。
	Rate int
}

// Result 是合成结果，Audio 为完整音频字节。
type Result struct {
	Audio []byte
	MIME  string
	// SampleRate 是 PCM 音频的采样率，非 PCM 时为 0。
	SampleRate int
}

// Provider 是语音合成实现，上层只依赖该接口以便替换服务商。
type Provider interface {
	Name() string
	Synthesize(ctx context.Context, req Request) (Result, error)
}
