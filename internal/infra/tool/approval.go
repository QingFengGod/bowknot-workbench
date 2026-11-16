package tool

import "context"

type approvalTokenKey struct{}

// WithApprovalToken 把审批令牌写入执行上下文。
// 上层在调用 Execute 前写入令牌，Approver 通过它把危险工具确认
// 关联到具体的 UI 审批请求。
func WithApprovalToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, approvalTokenKey{}, token)
}

// ApprovalToken 读取执行上下文中的审批令牌，未设置时返回空字符串。
func ApprovalToken(ctx context.Context) string {
	token, _ := ctx.Value(approvalTokenKey{}).(string)
	return token
}
