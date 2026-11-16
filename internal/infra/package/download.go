package pkgs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Download 把远程 tar 包下载到 dest 文件，限制最大体积并校验可选的
// SHA-256（十六进制，空串跳过校验）。
func Download(ctx context.Context, rawURL, dest string, sha256Hex string, maxSize int64, client *http.Client) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("下载地址无效：%s", rawURL)
	}
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("构造下载请求失败：%w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载包失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载包失败：HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxSize {
		return fmt.Errorf("包体积 %d 字节超过上限 %d 字节", resp.ContentLength, maxSize)
	}

	file, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("创建临时文件失败：%w", err)
	}

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(resp.Body, maxSize+1))
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("下载包失败：%w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭临时文件失败：%w", closeErr)
	}
	if written > maxSize {
		return fmt.Errorf("包体积超过上限 %d 字节", maxSize)
	}
	if sha256Hex != "" {
		expected, err := hex.DecodeString(strings.ToLower(sha256Hex))
		if err != nil {
			return fmt.Errorf("SHA-256 校验值格式无效：%q", sha256Hex)
		}
		if !equalDigest(digest.Sum(nil), expected) {
			return fmt.Errorf("SHA-256 校验失败，包可能被篡改")
		}
	}
	return nil
}

func equalDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
