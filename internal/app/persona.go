package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/store"
)

// assistantPersonaKey 是主助手人设在 app_meta 中的键。
const assistantPersonaKey = "assistant.persona"

// loadPersona 读取结构化人设；旧版纯文本人设按「用户特殊要求」兼容。
func loadPersona(ctx context.Context, st *store.Store) (contract.AssistantPersona, error) {
	raw, err := st.Meta.Get(ctx, assistantPersonaKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return contract.DefaultAssistantPersona(), nil
		}
		return contract.AssistantPersona{}, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return contract.DefaultAssistantPersona(), nil
	}

	var persona contract.AssistantPersona
	if err := json.Unmarshal([]byte(raw), &persona); err != nil {
		legacy := contract.DefaultAssistantPersona()
		legacy.Custom = raw
		return legacy, nil
	}
	return normalizePersona(persona), nil
}

// savePersona 保存结构化人设，姓名为空时拒绝保存。
func savePersona(ctx context.Context, st *store.Store, persona contract.AssistantPersona) error {
	persona = normalizePersona(persona)
	if persona.Name == "" {
		return errors.New("人设姓名不能为空")
	}
	data, err := json.Marshal(persona)
	if err != nil {
		return fmt.Errorf("保存人设失败：%w", err)
	}
	if err := st.Meta.Set(ctx, assistantPersonaKey, string(data)); err != nil {
		return fmt.Errorf("保存人设失败：%w", err)
	}
	return nil
}

func normalizePersona(p contract.AssistantPersona) contract.AssistantPersona {
	p.Name = strings.TrimSpace(p.Name)
	p.Gender = strings.TrimSpace(p.Gender)
	p.Age = strings.TrimSpace(p.Age)
	p.Personality = strings.TrimSpace(p.Personality)
	p.Role = strings.TrimSpace(p.Role)
	p.UserName = strings.TrimSpace(p.UserName)
	p.Custom = strings.TrimSpace(p.Custom)
	return p
}

// parsePersonaText 解析 Agent 的人设文本。历史数据是自由文本，
// 统一放进「其它要求」，保证旧记录仍能正常渲染。
func parsePersonaText(raw string) contract.AssistantPersona {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return contract.AssistantPersona{}
	}
	if strings.HasPrefix(raw, "{") {
		var persona contract.AssistantPersona
		if err := json.Unmarshal([]byte(raw), &persona); err == nil {
			return persona
		}
	}
	return contract.AssistantPersona{Custom: raw}
}

// personaText 把结构化人设序列化为存储文本。
func personaText(persona contract.AssistantPersona) string {
	data, err := json.Marshal(persona)
	if err != nil {
		return ""
	}
	return string(data)
}

// defaultAgentProfile 用名称与职责构造一份最小可用的独立人设。
func defaultAgentProfile(name, description string) contract.AssistantPersona {
	profile := contract.AssistantPersona{Name: strings.TrimSpace(name)}
	description = strings.TrimSpace(description)
	if description != "" && description != "暂无描述" {
		profile.Role = description
	}
	return profile
}

// personaPrompt 把人设渲染成提示词片段，空字段不写进提示词。
func personaPrompt(p contract.AssistantPersona) string {
	lines := make([]string, 0, 6)
	add := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			lines = append(lines, label+"："+value)
		}
	}
	add("姓名", p.Name)
	add("性别", p.Gender)
	add("年龄", p.Age)
	add("性格", p.Personality)
	add("角色或职责", p.Role)
	add("对用户的称呼", p.UserName)
	add("其它要求", p.Custom)
	if len(lines) == 0 {
		return ""
	}
	return "【你的人设】\n" + strings.Join(lines, "\n")
}

// personalGreetingText 把记忆或模型输出里的第三人称「用户」改成对用户的称呼，
// 避免问候语里出现「用户喜欢…」这种旁白式表达。
func personalGreetingText(text, userName string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	call := strings.TrimSpace(userName)
	if call == "" {
		call = "你"
	}
	text = strings.ReplaceAll(text, "用户本人", call)
	text = strings.ReplaceAll(text, "用户", call)
	return text
}

// personaPatch 是模型通过 updatePersona 工具提交的字段集合，
// 指针为 nil 表示本次不修改该字段。
type personaPatch struct {
	Name        *string `json:"name"`
	Gender      *string `json:"gender"`
	Age         *string `json:"age"`
	Personality *string `json:"personality"`
	Role        *string `json:"role"`
	UserName    *string `json:"userName"`
	Custom      *string `json:"custom"`
}

// applyPersonaPatch 把非空更新合并到当前人设。
func applyPersonaPatch(current contract.AssistantPersona, patch personaPatch) contract.AssistantPersona {
	set := func(dst *string, value *string) {
		if value == nil {
			return
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed == "" {
			return
		}
		*dst = trimmed
	}
	set(&current.Name, patch.Name)
	set(&current.Gender, patch.Gender)
	set(&current.Age, patch.Age)
	set(&current.Personality, patch.Personality)
	set(&current.Role, patch.Role)
	set(&current.UserName, patch.UserName)
	set(&current.Custom, patch.Custom)
	return normalizePersona(current)
}
