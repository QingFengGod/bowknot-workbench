//go:build windows

package pet

import "syscall"

var (
	user32             = syscall.NewLazyDLL("user32.dll")
	procReleaseCapture = user32.NewProc("ReleaseCapture")
	procSendMessageW   = user32.NewProc("SendMessageW")
)

const (
	wmNCLButtonDown = 0x00A1
	htCaption       = 2
)

// startNativeWindowDrag 把窗口移动交给系统，与 mygo 的 DragWindow 相同：
// 释放鼠标捕获后发送非客户区按下，进入系统移动循环，松手才返回。
// 这样拖动跟手，又不会像 DragWindow 那样在按下瞬间吞掉点击判定。
func startNativeWindowDrag(hwnd uintptr) bool {
	procReleaseCapture.Call()
	procSendMessageW.Call(hwnd, wmNCLButtonDown, htCaption, 0)
	return true
}
