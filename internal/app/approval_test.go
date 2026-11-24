package app

import (
	"context"
	"testing"
	"time"

	"bowknot-workbench/internal/infra/tool"
)

// TestApprovalResolveBeforeApprove 验证用户比工具执行更快时结果不会丢：
// Resolve 先到，Approve 后到时仍能读到放行结果。
func TestApprovalResolveBeforeApprove(t *testing.T) {
	hub := NewApprovalHub()
	token := hub.Register("run-1", "call-1")
	hub.Resolve("run-1", "call-1", true)

	ctx := tool.WithApprovalToken(context.Background(), token)
	if !hub.Approve(ctx, tool.Call{}) {
		t.Fatal("提前回传的审批结果被丢弃")
	}
}

// TestApprovalAutoApprove 验证无人值守任务可以直接放行危险工具。
func TestApprovalAutoApprove(t *testing.T) {
	hub := NewApprovalHub()
	token := hub.Register("run-2", "call-2")
	hub.Resolve("run-2", "call-2", true)

	ctx, cancel := context.WithTimeout(tool.WithApprovalToken(context.Background(), token), time.Second)
	defer cancel()
	if !hub.Approve(ctx, tool.Call{}) {
		t.Fatal("自动放行未生效")
	}
}

// TestApprovalDrop 验证清理后不会再误放行。
func TestApprovalDrop(t *testing.T) {
	hub := NewApprovalHub()
	token := hub.Register("run-3", "call-3")
	hub.Drop("run-3", "call-3")

	ctx := tool.WithApprovalToken(context.Background(), token)
	if hub.Approve(ctx, tool.Call{}) {
		t.Fatal("已清理的审批不应放行")
	}
}
