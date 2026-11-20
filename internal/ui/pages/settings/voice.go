package settings

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/components"
)

// fillTTSEditors 把语音合成快照同步到数字输入框，并清空密钥输入。
func (p *Page) fillTTSEditors() {
	p.ttsKeyInput = ""
	p.ttsRate = intText(p.tts.SampleRate)
	p.ttsSpeed = intText(p.tts.SpeechRate)
}

// voiceSection 渲染豆包语音合成配置，保存后写入 config.json。
func (p *Page) voiceSection(c *ui.Context) {
	t := c.Theme()

	if p.models == nil {
		components.Card(c, components.CardProps{}, func() {
			ui.Text(c, "语音合成服务不可用。").FontSize(13).TextColor(t.TextMuted)
		})
		return
	}

	components.Card(c, components.CardProps{Gap: 10}, func() {
		ui.Text(c, "豆包语音合成 2.0").FontSize(13).Bold()
		ui.Text(c, "保存后立即生效；未填写 API Key 时点击桌宠不会发声。").
			FontSize(11).
			TextColor(t.TextMuted)

		ui.Checkbox(c, &p.tts.Enabled, "启用语音合成")

		ui.Text(c, "Base URL").FontSize(12).Bold().TextColor(t.TextMuted)
		ui.TextInput(c, &p.tts.BaseURL).
			Placeholder("https://openspeech.bytedance.com/api/v3/plan/tts/unidirectional").
			MaxLength(300)

		ui.Text(c, "API Key").FontSize(12).Bold().TextColor(t.TextMuted)
		placeholder := "输入方舟 Agent Plan 专属 API Key"
		if p.tts.HasKey {
			placeholder = "已保存 " + p.tts.APIKey + "，留空保持不变"
		}
		ui.TextInput(c, &p.ttsKeyInput).
			Password().
			Placeholder(placeholder).
			MaxLength(300)
		if p.tts.HasKey {
			ui.Checkbox(c, &p.tts.ClearKey, "清除已保存的 API Key")
		}

		ui.Row(c).FillWidth().Gap(10).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "音色 ID").FontSize(12).Bold().TextColor(t.TextMuted)
				ui.TextInput(c, &p.tts.Speaker).
					Placeholder("zh_female_vv_uranus_bigtts").
					MaxLength(100)
			})
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "模型版本").FontSize(12).Bold().TextColor(t.TextMuted)
				ui.TextInput(c, &p.tts.ResourceID).
					Placeholder("seed-tts-2.0").
					MaxLength(60)
			})
		})

		ui.Row(c).FillWidth().Gap(10).Children(func() {
			p.numberField(c, "采样率（Hz）", "24000", &p.ttsRate, 6)
			p.numberField(c, "语速（-50–100）", "0", &p.ttsSpeed, 4)
		})
		ui.Text(c, "音频格式固定为 PCM（16 位单声道），下一次播报即使用新配置。").
			FontSize(11).
			TextColor(t.TextMuted)

		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if components.Button(c, components.ButtonProps{
				Label:   "保存",
				Variant: components.ButtonPrimary,
			}).Clicked() {
				p.saveTTS()
			}
		})
	})
}

func (p *Page) saveTTS() {
	if p.models == nil {
		p.setError("设置服务不可用")
		return
	}
	rate, ok := p.requiredInt("采样率", p.ttsRate)
	if !ok {
		return
	}
	speed, ok := p.requiredInt("语速", p.ttsSpeed)
	if !ok {
		return
	}
	if rate <= 0 {
		p.setError("采样率需大于 0")
		return
	}
	if speed < -50 || speed > 100 {
		p.setError("语速需在 -50 到 100 之间")
		return
	}

	draft := p.tts
	draft.APIKeyInput = strings.TrimSpace(p.ttsKeyInput)
	draft.SampleRate = rate
	draft.SpeechRate = speed
	if err := p.models.SaveTTS(draft); err != nil {
		p.setError(err.Error())
		return
	}

	p.tts = p.models.TTS()
	p.fillTTSEditors()
	p.setNotice("语音合成配置已保存，下一次点击桌宠生效。")
}
