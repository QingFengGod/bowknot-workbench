//go:build windows

package tool

import (
	"os/exec"
	"syscall"
)

// hideProcessWindow 隐藏插件子进程的控制台窗口，避免桌面端弹出黑框。
func hideProcessWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
