package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/ui/theme"
)

// ComposerProps 描述输入区。组件只负责结构与输入交互，发送逻辑由调用方提供。
type ComposerProps struct {
	Draft *string
	// FocusRequested 为 true 时在下一帧聚焦输入框，组件读取后置回 false。
	FocusRequested *bool
	Placeholder    string
	// Error 非空时显示在输入框上方。
	Error string
	// Submit 处理发送（Enter），返回 true 表示已发送，组件随后清空输入框。
	Submit func(text string) bool
	// Cancel 处理 Esc，为 nil 时不拦截。
	Cancel func()
	// OnInput 在输入内容变化时回调，通常用来清除上一次的错误提示。
	OnInput func()
	// Attachments 渲染输入框内的待发送附件区，为 nil 时不构建。
	Attachments func(c *ui.Context)
	// OnPaste 在输入区粘贴时调用，返回 true 表示调用方已接管这次粘贴（例如粘贴图片为附件）。
	OnPaste func(c *ui.Context, text string) bool
	// Actions 是底部操作区插槽：模型选择、发送/停止等由调用方决定。
	// 内容默认靠右，需要左侧控件时先在插槽里放一个 Grow 的占位。
	Actions func(c *ui.Context)
}

// Composer 是对话类页面的统一输入区：带阴影的输入框、焦点描边、
// Enter 发送与 Esc 取消；底部操作区通过 Actions 插槽注入，避免写死业务按钮。
func Composer(c *ui.Context, p ComposerProps) {
	t := c.Theme()

	ui.Column(c).
		FillWidth().
		Shrink(0).
		Padding(t.Space(3)).
		Gap(t.Space(3)).
		Background(t.Surface).
		Children(func() {
			if p.Error != "" {
				ui.Text(c, p.Error).FontSize(12).TextColor(t.Danger)
			}

			box := ui.Column(c).
				FillWidth().
				Padding(t.Space(3)).
				Radius(theme.Radius).
				Background(t.Surface).
				BorderWidth(1).
				BorderColor(t.Border).
				Gap(6)
			theme.NormalShadow.Apply(c, box)
			if box.FocusWithin() {
				box.BorderColor(t.Accent)
			}

			box.Children(func() {
				if p.Attachments != nil {
					p.Attachments(c)
				}
				input := ui.TextAreaBase(c, p.Draft).
					Placeholder(p.Placeholder).
					Lines(2, 8).
					FillWidth().
					FontSize(14)
				input.OnPaste(func(text string) bool {
					if p.OnPaste != nil {
						return p.OnPaste(c, text)
					}
					return false
				})
				input.OnTextKey(func(k ui.TextKey) bool {
					// 部分输入法会吞掉 Ctrl+V，这里先接管一次，避免图片粘贴失效；
					// 没有附件可加时返回 false，让文本照常粘贴。
					if p.OnPaste != nil && k.Key == ui.KeyV && k.Mods == ui.Cmd {
						return p.OnPaste(c, k.Text())
					}
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						if p.Submit != nil && p.Submit(k.Text()) {
							k.SetText("")
						}
						return true
					}
					if k.Key == ui.KeyEscape && p.Cancel != nil {
						p.Cancel()
						return true
					}
					return false
				})
				if input.Changed() && p.OnInput != nil {
					p.OnInput()
				}
				if p.FocusRequested != nil && *p.FocusRequested {
					input.Focus()
					*p.FocusRequested = false
				}

				if p.Actions == nil {
					return
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.End).Gap(t.Space(3)).Children(func() {
					p.Actions(c)
				})
			})
		})
}
