package components

import (
	"bytes"
	"errors"
	"image/color"
	"image/png"

	"github.com/egoist/mygo/ui"
)

// LogoPNG 把品牌 logo 渲染成指定边长的 PNG，供托盘等系统图标使用。
// tint 通常传品牌色 theme.Brand。
func LogoPNG(size int, tint ui.Color) ([]byte, error) {
	svg := icons[IconLogo]
	if svg == nil {
		return nil, errors.New("缺少品牌图标")
	}
	var buf bytes.Buffer
	current := color.NRGBA{R: tint.R, G: tint.G, B: tint.B, A: tint.A}
	if err := png.Encode(&buf, svg.Image(size, size, current)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
