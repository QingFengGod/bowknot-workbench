package components

import "github.com/egoist/mygo/ui"

// Page 是原生页面的统一容器：铺满内容区，负责背景、内边距与间距。
func Page(c *ui.Context, children func()) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Gap(t.Space(3)).Children(children)
}
