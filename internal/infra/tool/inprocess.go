package tool

import (
	"context"
	"fmt"
)

// InProcess 是首版占位执行器：不执行真实逻辑，只回显调用信息，
// 用于验证注册、授权、审批与审计链路。
type InProcess struct{}

func (InProcess) Execute(ctx context.Context, call Call) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	args := call.Arguments
	if args == "" {
		args = "{}"
	}
	return Result{
		Content: fmt.Sprintf("工具 %s 已被调用（transport=%s，arguments=%s）。当前为 in-process 占位实现，真实执行逻辑将在后续阶段接入。",
			call.Tool.ID, call.Transport, args),
	}, nil
}
