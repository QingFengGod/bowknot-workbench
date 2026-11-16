package pkgs

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxPackagePathLen 限制解包后的完整路径长度，规避 Windows MAX_PATH 问题。
const maxPackagePathLen = 240

// ExtractTar 把 tar 归档解包到 dest 目录。
// 只接受普通文件与目录；拒绝绝对路径、..、反斜杠、符号链接、硬链接、
// 设备文件与超长路径，累计解包体积超过 maxTotal 时立即中止。
func ExtractTar(r io.Reader, dest string, maxTotal int64) error {
	reader := tar.NewReader(r)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取包内容失败：%w", err)
		}

		switch header.Typeflag {
		case tar.TypeDir, tar.TypeReg:
		default:
			// 符号链接、硬链接、字符/块设备、FIFO 一律拒绝，防止 tar slip。
			return fmt.Errorf("包内包含不允许的条目类型（type=%q）：%s", string(rune(header.Typeflag)), header.Name)
		}

		target, err := safeJoin(dest, header.Name)
		if err != nil {
			return err
		}
		if len(target) > maxPackagePathLen {
			return fmt.Errorf("包内路径过长（%d 字符）：%s", len(target), header.Name)
		}

		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("创建包目录失败：%w", err)
			}
			continue
		}

		if header.Size > maxTotal-total {
			return fmt.Errorf("包体积超过上限（%d 字节）", maxTotal)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("创建包目录失败：%w", err)
		}
		mode := header.FileInfo().Mode().Perm()
		if mode == 0 {
			// 归档未记录权限时兜底为普通可写文件，避免 Windows 下落成只读文件。
			mode = 0o644
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return fmt.Errorf("写入包文件失败：%w", err)
		}
		written, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		total += written
		if copyErr != nil {
			return fmt.Errorf("写入包文件失败：%w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("关闭包文件失败：%w", closeErr)
		}
	}
}

// safeJoin 校验包内路径并拼接到 dest 下。
func safeJoin(dest, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("包内条目名无效：%q", name)
	}
	if strings.Contains(name, "\\") {
		return "", fmt.Errorf("包内条目名包含反斜杠：%q", name)
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") || (len(name) > 1 && name[1] == ':') {
		return "", fmt.Errorf("包内包含绝对路径：%s", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("包内包含 .. 路径：%s", name)
		}
	}
	cleaned := filepath.Clean(filepath.Join(dest, name))
	if cleaned != filepath.Clean(dest) && !strings.HasPrefix(cleaned, filepath.Clean(dest)+string(filepath.Separator)) {
		return "", fmt.Errorf("包内路径越界：%s", name)
	}
	return cleaned, nil
}

// CopyTree 把本地目录复制到 dest，只复制普通文件与目录，
// 遇到符号链接等其他类型直接报错。总大小受 maxTotal 限制。
func CopyTree(src, dest string, maxTotal int64) error {
	var total int64
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, 0o755)
		case entry.Type().IsRegular():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() > maxTotal-total {
				return fmt.Errorf("包体积超过上限（%d 字节）", maxTotal)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("读取包文件失败：%w", err)
			}
			total += int64(len(data))
			return os.WriteFile(target, data, info.Mode().Perm())
		default:
			return fmt.Errorf("包内包含不允许的条目类型：%s", path)
		}
	})
}

// MeasureDir 统计目录下普通文件的总字节数；目录不存在时返回 0。
func MeasureDir(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
