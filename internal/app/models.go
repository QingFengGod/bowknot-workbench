package app

import (
	"errors"

	"bowknot-workbench/internal/contract"
)

// modelName 在配置的模型列表里查找显示名；找不到时直接显示 key。
func modelName(models []contract.ChatModel, id string) string {
	if id == "" {
		return "默认模型"
	}
	for _, item := range models {
		if item.ID == id {
			return item.Name
		}
	}
	return id
}

// modelAvailable 校验模型是否在配置列表中；列表为空（未配置模型服务）时放行，
// 交给网关按其默认模型处理。
func modelAvailable(models []contract.ChatModel, id string) error {
	if len(models) == 0 || id == "" {
		return nil
	}
	for _, item := range models {
		if item.ID == id {
			return nil
		}
	}
	return errors.New("所选模型不可用，请重新选择模型")
}
