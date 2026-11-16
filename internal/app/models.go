package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/llm"
)

// ResolveModels 在启动时缓存可用模型列表：优先使用网关返回的真实模型，
// 查询失败或为空时退回配置文件里的默认模型，避免 UI 出现空列表。
func ResolveModels(ctx context.Context, provider llm.Provider, defaultModel string, logger *slog.Logger) []contract.ChatModel {
	if provider != nil {
		infos, err := provider.Models(ctx)
		switch {
		case err != nil:
			if logger != nil {
				logger.Warn("获取模型列表失败，改用配置的默认模型", "error", err)
			}
		default:
			out := make([]contract.ChatModel, 0, len(infos))
			for _, info := range infos {
				id := strings.TrimSpace(info.ID)
				if id == "" {
					continue
				}
				name := strings.TrimSpace(info.Name)
				if name == "" {
					name = id
				}
				out = append(out, contract.ChatModel{ID: id, Name: name})
			}
			if len(out) > 0 {
				return out
			}
		}
	}

	if defaultModel = strings.TrimSpace(defaultModel); defaultModel != "" {
		return []contract.ChatModel{{ID: defaultModel, Name: defaultModel, Description: "配置的默认模型"}}
	}
	return nil
}

// modelName 在缓存的模型列表里查找显示名；找不到时直接显示 ID，
// 不再把未知模型静默映射成演示名称。
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

// modelAvailable 校验模型是否在缓存列表中；列表为空（未配置模型服务）时放行，
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
