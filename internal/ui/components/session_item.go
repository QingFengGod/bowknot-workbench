package components

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/format"
)

// SessionItemProps 描述会话列表项。
type SessionItemProps struct {
	Title     string
	Summary   string
	UpdatedAt int64
	Active    bool
	Pinned    bool
	// MenuOpen 是「更多」菜单的开关，由调用方按行持有并保证每帧指向同一地址。
	MenuOpen *bool
	// Status 非空时在标题后显示状态标签，用于任务线程。
	Status     string
	StatusTone Tone
	// DeleteDisabled 为 true 时菜单里的删除项置灰。
	DeleteDisabled bool
	OnSelect       func()
	OnRename       func()
	OnTogglePin    func()
	OnDelete       func()
}

// SessionItem 是会话列表中的一行：标题、相对时间、摘要与更多操作菜单。
// 选中态用品牌色左侧竖条与浅底表示，回调由调用方提供。
func SessionItem(c *ui.Context, p SessionItemProps) {
	t := c.Theme()

	row := ui.Column(c).
		FillWidth().
		Padding(t.Space(3)).
		Radius(8).
		Gap(1).
		BorderWidth(0, 0, 0, 3).
		BorderColor(ui.Transparent)
	switch {
	case p.Active:
		row.Background(t.Accent.Alpha(0.1)).BorderColor(t.Accent)
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}

	// handled 防止「更多」里的操作同时触发整行的选中。
	handled := false
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, p.Title).FontSize(13).Bold().MaxLines(1).Grow(1)
			if p.Status != "" {
				Chip(c, p.Status, p.StatusTone)
			}
			ui.Text(c, format.Relative(p.UpdatedAt)).FontSize(11).TextColor(t.TextMuted)

			more := ui.ButtonBase(c).
				Width(20).
				Height(20).
				Radius(4).
				AlignItems(ui.Center).
				Justify(ui.Center).
				TextColor(t.TextMuted)
			if more.Hovered() {
				more.Background(t.Surface).TextColor(t.Text)
			}
			more.Children(func() {
				Icon(c, IconMore, 13)
			})
			more.Tooltip("更多操作")
			if more.Clicked() {
				handled = true
				*p.MenuOpen = true
			}
			Popover(c, more, p.MenuOpen, func() {
				PopoverItem(c, PopoverItemProps{
					Label: "重命名",
					OnClick: func() {
						handled = true
						*p.MenuOpen = false
						if p.OnRename != nil {
							p.OnRename()
						}
					},
					Content: func(c *ui.Context, _ bool) {
						ui.Text(c, "重命名").FontSize(13).TextColor(c.Theme().Text).MaxLines(1)
					},
				})

				pinLabel := "置顶"
				if p.Pinned {
					pinLabel = "取消置顶"
				}
				PopoverItem(c, PopoverItemProps{
					Label: pinLabel,
					OnClick: func() {
						handled = true
						*p.MenuOpen = false
						if p.OnTogglePin != nil {
							p.OnTogglePin()
						}
					},
					Content: func(c *ui.Context, _ bool) {
						ui.Text(c, pinLabel).FontSize(13).TextColor(c.Theme().Text).MaxLines(1)
					},
				})

				ui.Box(c).FillWidth().Height(1).Background(t.Border)

				PopoverItem(c, PopoverItemProps{
					Label:    "删除",
					Danger:   true,
					Disabled: p.DeleteDisabled,
					OnClick: func() {
						handled = true
						*p.MenuOpen = false
						if p.OnDelete != nil {
							p.OnDelete()
						}
					},
					Content: func(c *ui.Context, _ bool) {
						ui.Text(c, "删除").FontSize(13).TextColor(c.Theme().Danger).MaxLines(1)
					},
				})
			})
		})

		summary := strings.TrimSpace(p.Summary)
		if summary == "" {
			summary = "还没有消息"
		}
		ui.Text(c, summary).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
	})

	if row.Clicked() && !handled && p.OnSelect != nil {
		p.OnSelect()
	}
}
