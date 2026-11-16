package app

import (
	"context"
	"sync"

	"bowknot-workbench/internal/infra/tool"
)

// ApprovalHub 把危险工具的审批请求与 UI 回传连接起来。
// 令牌由 runID 与 callID 组成，同一 Run 内跨会话也不会串线。
type ApprovalHub struct {
	mu      sync.Mutex
	pending map[string]chan bool
}

func NewApprovalHub() *ApprovalHub {
	return &ApprovalHub{pending: map[string]chan bool{}}
}

// Register 在发出审批事件前登记，返回执行时写入上下文的令牌。
// 先登记再发事件，避免用户快速确认时找不到等待中的请求。
func (h *ApprovalHub) Register(runID, callID string) string {
	token := approvalToken(runID, callID)
	h.mu.Lock()
	h.pending[token] = make(chan bool, 1)
	h.mu.Unlock()
	return token
}

// Resolve 回传用户选择；找不到等待中的请求时忽略。
func (h *ApprovalHub) Resolve(runID, callID string, approved bool) {
	token := approvalToken(runID, callID)
	h.mu.Lock()
	ch := h.pending[token]
	delete(h.pending, token)
	h.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- approved:
	default:
	}
}

// Approve 实现 tool.Approver：按执行上下文里的令牌等待用户确认，
// 上下文取消或没有登记时一律拒绝。
func (h *ApprovalHub) Approve(ctx context.Context, _ tool.Call) bool {
	token := tool.ApprovalToken(ctx)
	if token == "" {
		return false
	}
	h.mu.Lock()
	ch := h.pending[token]
	h.mu.Unlock()
	if ch == nil {
		return false
	}
	defer func() {
		h.mu.Lock()
		delete(h.pending, token)
		h.mu.Unlock()
	}()

	select {
	case approved := <-ch:
		return approved
	case <-ctx.Done():
		return false
	}
}

func approvalToken(runID, callID string) string {
	return runID + "/" + callID
}
