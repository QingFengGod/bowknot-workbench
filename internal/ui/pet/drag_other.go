//go:build !windows

package pet

// startNativeWindowDrag 在非 Windows 平台不可用，调用方回退到逐帧跟随。
func startNativeWindowDrag(uintptr) bool { return false }
