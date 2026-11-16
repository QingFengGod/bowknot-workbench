// Package id 生成按时间递增的本地唯一标识。
package id

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// New 返回「13 位毫秒时间戳 + 13 位随机 base32」的 26 位标识。
// 同一毫秒内随机部分不同，跨毫秒时整体字符串按时间递增。
func New() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// 随机源不可用时退化为纳秒时间，仍保持固定长度。
		binary.LittleEndian.PutUint64(buf[:], uint64(time.Now().UnixNano()))
	}
	random := strings.ToLower(encoding.EncodeToString(buf[:]))
	return fmt.Sprintf("%013d%s", time.Now().UnixMilli(), random)
}
