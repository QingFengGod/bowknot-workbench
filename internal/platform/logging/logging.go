// Package logging 初始化统一的结构化日志输出。
// app.log 超过 maxLogSize 时轮转，保留最近 maxLogFiles 个历史文件。
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	fileName    = "app.log"
	rotatedGlob = "app-*.log"
)

// 包级变量便于测试调整轮转阈值。
var (
	maxLogSize  = int64(10 << 20) // 单个日志文件上限 10MB
	maxLogFiles = 5               // 轮转后保留的历史文件数
)

type Options struct {
	Dir   string
	Level slog.Level
}

// New 同时输出到 stderr 与轮转的日志文件，返回日志器与关闭函数。
func New(opts Options) (*slog.Logger, func() error, error) {
	if strings.TrimSpace(opts.Dir) == "" {
		return nil, nil, fmt.Errorf("日志目录不能为空")
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("创建日志目录失败：%w", err)
	}

	writer, err := newRotatingWriter(opts.Dir)
	if err != nil {
		return nil, nil, err
	}

	handler := slog.NewTextHandler(io.MultiWriter(os.Stderr, writer), &slog.HandlerOptions{
		Level: opts.Level,
	})
	return slog.New(handler), func() error { return writer.Close() }, nil
}

// rotatingWriter 在单个文件超过 maxLogSize 时把它改名留档并开新文件，
// 避免长时间运行后日志无限增长。
type rotatingWriter struct {
	dir  string
	path string
	file *os.File
	size int64
}

func newRotatingWriter(dir string) (*rotatingWriter, error) {
	w := &rotatingWriter{
		dir:  dir,
		path: filepath.Join(dir, fileName),
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	// 上次运行遗留的文件已达上限时先轮转，避免本次启动直接写满。
	if info, err := w.file.Stat(); err == nil && info.Size() >= maxLogSize {
		w.rotate()
	}
	return w, nil
}

func (w *rotatingWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("打开日志文件失败：%w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("读取日志文件信息失败：%w", err)
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	if w.size+int64(len(p)) > maxLogSize {
		w.rotate()
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() {
	w.file.Close()
	// 改名失败只影响归档，不影响继续写新文件。
	os.Rename(w.path, w.archivePath())
	if err := w.open(); err != nil {
		// 打不开就重建空文件，保证写路径不为 nil。
		w.file, _ = os.Create(w.path)
		w.size = 0
	}
	w.prune()
}

// archivePath 生成轮转归档路径；同一秒内多次轮转时追加序号避免互相覆盖。
func (w *rotatingWriter) archivePath() string {
	stamp := time.Now().Format("20060102-150405")
	path := filepath.Join(w.dir, "app-"+stamp+".log")
	for i := 1; ; i++ {
		if _, err := os.Stat(path); err != nil {
			return path
		}
		path = filepath.Join(w.dir, fmt.Sprintf("app-%s-%d.log", stamp, i))
	}
}

// prune 只保留最近 maxLogFiles 个轮转文件。
func (w *rotatingWriter) prune() {
	matches, err := filepath.Glob(filepath.Join(w.dir, rotatedGlob))
	if err != nil || len(matches) <= maxLogFiles {
		return
	}
	sort.Strings(matches)
	for _, path := range matches[:len(matches)-maxLogFiles] {
		os.Remove(path)
	}
}

func (w *rotatingWriter) Close() error {
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

// ParseLevel 把配置里的级别名称转成 slog 级别，未知值回退为 info。
func ParseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
