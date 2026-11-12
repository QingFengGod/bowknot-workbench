package format

import (
	"fmt"
	"time"
)

// Time 把 Unix 毫秒格式化为「2006-01-02 15:04」。
func Time(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04")
}

// Clock 把 Unix 毫秒格式化为「15:04」，用于消息时间。
func Clock(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).Format("15:04")
}

// Relative 返回与 Web 端一致的紧凑相对时间。
func Relative(ms int64) string {
	if ms == 0 {
		return ""
	}
	now := time.Now()
	t := time.UnixMilli(ms)
	diff := now.Sub(t)

	if diff < time.Minute {
		return "刚刚"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%d 分钟", int(diff/time.Minute))
	}

	startOfDay := func(v time.Time) time.Time {
		return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, v.Location())
	}
	days := int(startOfDay(now).Sub(startOfDay(t)).Hours() / 24)
	switch {
	case days <= 0:
		return t.Format("15:04")
	case days == 1:
		return "昨天"
	case days < 7:
		return fmt.Sprintf("%d 天前", days)
	default:
		return fmt.Sprintf("%d/%d", int(t.Month()), t.Day())
	}
}

// Size 把字节数格式化为 B / KB / MB。
func Size(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1024/1024)
	}
}
