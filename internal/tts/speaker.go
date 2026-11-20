package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"

	"github.com/egoist/mygo/audio"
)

// Speaker 串行合成并播放语音，新播报会打断上一条。
type Speaker struct {
	provider Provider
	onStart  func()

	mu      sync.Mutex
	current *audio.Player
	// finish 关闭当前播报的完成信号，打断与自然结束都只触发一次。
	finish func()
}

// NewSpeaker 用给定合成实现创建播放器。
func NewSpeaker(provider Provider) *Speaker {
	return &Speaker{provider: provider}
}

// SetProvider 替换后续播报使用的合成实现，用于设置页保存后即时生效。
func (s *Speaker) SetProvider(provider Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = provider
}

// SetOnStart 注册播放开始时的回调，用于把角色切到说话动作。
func (s *Speaker) SetOnStart(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onStart = fn
}

// Speak 合成并播放文本，直到播放结束、被新播报打断或 ctx 结束才返回。
// 合成失败时不会打断正在播放的语音。
func (s *Speaker) Speak(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	s.mu.Lock()
	provider := s.provider
	onStart := s.onStart
	s.mu.Unlock()
	if provider == nil {
		return fmt.Errorf("语音合成未配置")
	}
	res, err := provider.Synthesize(ctx, Request{Text: text})
	if err != nil {
		return err
	}

	raw := res.Audio
	// 流式接口推荐 PCM，这里补 WAV 头交给标准解码器，避免引入 mp3 解码依赖。
	if res.MIME == "audio/L16" {
		if res.SampleRate <= 0 {
			return fmt.Errorf("语音合成缺少采样率")
		}
		raw = pcmToWAV(res.Audio, res.SampleRate)
	}

	player, err := audio.OpenStream(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解码合成音频失败：%w", err)
	}

	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done) }) }

	s.mu.Lock()
	prev, prevFinish := s.current, s.finish
	s.current, s.finish = player, finish
	s.mu.Unlock()

	if prev != nil {
		prev.Stop()
		prev.Close()
		if prevFinish != nil {
			prevFinish()
		}
	}

	player.OnEnd(finish)
	player.Play()
	if onStart != nil {
		onStart()
	}

	select {
	case <-done:
	case <-ctx.Done():
		s.Stop()
		<-done
	}
	return nil
}

// Stop 停止并释放当前播放器。
func (s *Speaker) Stop() {
	s.mu.Lock()
	player, finish := s.current, s.finish
	s.current, s.finish = nil, nil
	s.mu.Unlock()

	if player != nil {
		player.Stop()
		player.Close()
	}
	if finish != nil {
		finish()
	}
}

// pcmToWAV 把 16 位单声道 PCM 封装成 WAV，便于用内置 wav 解码器播放。
func pcmToWAV(pcm []byte, sampleRate int) []byte {
	const (
		headerSize = 44
		channels   = 1
		bits       = 16
	)
	byteRate := sampleRate * channels * bits / 8
	blockAlign := channels * bits / 8

	buf := make([]byte, headerSize, headerSize+len(pcm))
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+len(pcm)))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:24], channels)
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], bits)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(len(pcm)))
	return append(buf, pcm...)
}
