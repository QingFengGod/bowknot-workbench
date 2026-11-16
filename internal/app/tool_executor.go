package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/store"
)

// ToolExecutor 接管需要宿主真实执行的内置工具，其余工具继续走占位执行器。
type ToolExecutor struct {
	store *store.Store
}

func NewToolExecutor(st *store.Store) *ToolExecutor {
	return &ToolExecutor{store: st}
}

func (e *ToolExecutor) Execute(ctx context.Context, call tool.Call) (tool.Result, error) {
	if call.Tool.ID == tool.PersonaToolID {
		return e.updatePersona(ctx, call.Arguments)
	}
	return tool.InProcess{}.Execute(ctx, call)
}

// updatePersona 把人设工具的参数合并进当前人设，空字段保持原值。
func (e *ToolExecutor) updatePersona(ctx context.Context, arguments string) (tool.Result, error) {
	current, err := loadPersona(ctx, e.store)
	if err != nil {
		return tool.Result{}, err
	}

	var patch personaPatch
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &patch); err != nil {
			return tool.Result{}, fmt.Errorf("人设参数解析失败：%w", err)
		}
	}

	next := applyPersonaPatch(current, patch)
	if err := savePersona(ctx, e.store, next); err != nil {
		return tool.Result{}, err
	}
	return tool.Result{Content: "已更新助手人设：" + summarizePersona(next)}, nil
}

// summarizePersona 生成便于在对话里展示的一行人设摘要。
func summarizePersona(p contract.AssistantPersona) string {
	parts := make([]string, 0, 6)
	add := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, label+"「"+value+"」")
		}
	}
	add("姓名", p.Name)
	add("性别", p.Gender)
	add("年龄", p.Age)
	add("性格", p.Personality)
	add("角色", p.Role)
	add("对用户的称呼", p.UserName)
	add("其它要求", p.Custom)
	if len(parts) == 0 {
		return "暂无内容"
	}
	return strings.Join(parts, "，")
}
