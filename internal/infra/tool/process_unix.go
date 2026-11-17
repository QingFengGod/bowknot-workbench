//go:build !windows

package tool

import (
	"os/exec"
	"syscall"
)

// hideProcessWindow 在非 Windows 平台不需要额外处理；
// 设置独立进程组，便于宿主退出时整组回收。
func hideProcessWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
