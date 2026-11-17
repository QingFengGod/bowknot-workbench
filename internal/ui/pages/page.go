package pages

import "github.com/egoist/mygo/ui"

// Page 是一个原生页面组件，视图函数每帧重建。
type Page interface {
	View(c *ui.Context)
}

// Showable 是可选的页面生命周期：每次切换到该页时调用，
// 用于刷新在其它页面产生的外部变更（例如主助手创建了新的 Agent）。
type Showable interface {
	OnShow()
}
