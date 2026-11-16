package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogRotation(t *testing.T) {
	dir := t.TempDir()
	oldSize, oldFiles := maxLogSize, maxLogFiles
	maxLogSize = 64
	maxLogFiles = 2
	defer func() { maxLogSize, maxLogFiles = oldSize, oldFiles }()

	logger, closeLog, err := New(Options{Dir: dir, Level: slog.LevelInfo})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	for i := 0; i < 8; i++ {
		logger.Info(strings.Repeat("x", 64), "round", i)
	}
	if err := closeLog(); err != nil {
		t.Fatalf("close: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "app-*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != maxLogFiles {
		t.Fatalf("轮转文件数 = %d, want %d", len(matches), maxLogFiles)
	}
	if info, err := os.Stat(filepath.Join(dir, fileName)); err != nil || info.Size() >= maxLogSize+2048 {
		// 轮转发生在写入前，当前文件最多持有超过阈值后的一条完整记录。
		t.Fatalf("当前日志文件异常：%v, size=%d", err, info.Size())
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"WARN":  slog.LevelWarn,
		"error": slog.LevelError,
		"":      slog.LevelInfo,
		"其他":    slog.LevelInfo,
	}
	for input, want := range cases {
		if got := ParseLevel(input); got != want {
			t.Fatalf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}
