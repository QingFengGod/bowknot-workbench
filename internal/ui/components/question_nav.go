package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// QuestionNavItem 是导航上的一个点，对应一条用户提问。
type QuestionNavItem struct {
	// Title 用于悬浮提示，通常是提问内容的摘要。
	Title string
	// Target 是消息行元素，点击时滚动到它。
	Target ui.Element
}

// QuestionNavProps 描述会话右侧的用户提问导航。
type QuestionNavProps struct {
	Items []QuestionNavItem
	// List 是消息列表的滚动容器，用来判断当前视口里的提问。
	List ui.Element
	// AtEnd 表示列表已经滚到底部：此时最后一个提问未必能顶到视口顶部，
	// 仍应把它算作激活项。
	AtEnd bool
	// OnSelect 在跳转前调用，页面可借此关闭自动贴底。
	OnSelect func()
}

// QuestionNav 在右侧画一列圆点，悬浮显示提问摘要，点击把对应提问滚到顶部。
// 少于两条提问时不显示，避免单条会话出现无意义的导航。
func QuestionNav(c *ui.Context, p QuestionNavProps) {
	if len(p.Items) < 2 {
		return
	}
	t := c.Theme()
	active := activeQuestion(p.List, p.Items, p.AtEnd)

	rail := ui.Column(c).
		Absolute().
		Right(t.Space(2)).
		Top(0).
		Bottom(0).
		Justify(ui.Center).
		Gap(t.Space(2))
	rail.Children(func() {
		for i, item := range p.Items {
			dot := ui.Box(c).Width(8).Height(8).Radius(4).Cursor(ui.CursorPointer)
			if i == active || dot.Hovered() {
				dot.Background(t.Accent)
			} else {
				dot.Background(t.TextMuted.Alpha(0.35))
			}
			dot.Tooltip(questionTitle(item.Title))

			if dot.Clicked() {
				if p.OnSelect != nil {
					p.OnSelect()
				}
				item.Target.ScrollIntoView(ui.ScrollIntoViewOptions{
					Block:     ui.ScrollStart,
					Animation: &ui.Smooth,
				})
			}
		}
	})
}

// activeQuestion 找出视口顶部所在的那条提问，用上一帧的布局位置判断。
// 已经滚到底部时最后一个提问无法顶到视口顶部，直接算最后一条。
func activeQuestion(list ui.Element, items []QuestionNavItem, atEnd bool) int {
	if atEnd {
		return len(items) - 1
	}

	area := list.Bounds()
	if area.H <= 0 {
		return -1
	}

	active := 0
	for i, item := range items {
		if item.Target.Bounds().Y-area.Y <= 48 {
			active = i
		}
	}
	return active
}

// questionTitle 把多行提问压成单行摘要，避免提示框过长。
func questionTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > 80 {
		s = string(runes[:80]) + "…"
	}
	return s
}
