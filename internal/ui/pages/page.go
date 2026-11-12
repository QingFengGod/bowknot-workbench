package pages

import "github.com/egoist/mygo/ui"

// Page 是一个原生页面组件，视图函数每帧重建。
type Page interface {
	View(c *ui.Context)
}
