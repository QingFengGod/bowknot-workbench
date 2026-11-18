package settings

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/theme"
)

// modelsSection 渲染服务商与模型两块内容。
func (p *Page) modelsSection(c *ui.Context) {
	t := c.Theme()

	ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, "服务商").FontSize(14).Bold()
			ui.Text(c, fmt.Sprintf("已配置 %d 个服务商 · %d 个模型", len(p.providers), len(p.modelList))).
				FontSize(11).
				TextColor(t.TextMuted)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if components.Button(c, components.ButtonProps{
				Label:   "添加服务商",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.openProviderDialog("")
			}
			if components.Button(c, components.ButtonProps{
				Label:   "拉取模型",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.openFetchDialog()
			}
			if components.Button(c, components.ButtonProps{
				Label:   "手动添加模型",
				Variant: components.ButtonPrimary,
			}).Clicked() {
				p.openModelDialog("", "")
			}
		})
	})

	if len(p.providers) == 0 {
		components.Card(c, components.CardProps{}, func() {
			ui.Text(c, "还没有配置服务商。").FontSize(13).Bold()
			ui.Text(c, "添加服务商后即可拉取或录入模型，未配置时应用使用演示模型。").
				FontSize(12).
				TextColor(t.TextMuted)
		})
		return
	}

	for _, provider := range p.providers {
		p.providerCard(c, provider)
	}

	ui.Text(c, "模型").FontSize(14).Bold()
	if len(p.modelList) == 0 {
		components.Card(c, components.CardProps{}, func() {
			ui.Text(c, "还没有可用模型。").FontSize(13).Bold()
			ui.Text(c, "在上方服务商卡片里点击「拉取模型」，或手动添加模型。").
				FontSize(12).
				TextColor(t.TextMuted)
		})
		return
	}

	for _, provider := range p.providers {
		p.providerModels(c, provider)
	}
}

func (p *Page) providerCard(c *ui.Context, provider contract.ProviderConfig) {
	t := c.Theme()

	components.Card(c, components.CardProps{}, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				components.Avatar(c, provider.Name, 32)
				ui.Column(c).Gap(3).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, provider.Name).FontSize(13).Bold().MaxLines(1)
						if !provider.Enabled {
							components.Chip(c, "已停用", components.ToneNeutral)
						}
					})

					baseURL := provider.BaseURL
					if baseURL == "" {
						baseURL = "未填写 BaseURL"
					}
					ui.Text(c, baseURL).FontSize(11).TextColor(t.TextMuted).MaxLines(1)

					ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
						if provider.HasKey {
							components.Chip(c, "密钥已配置", components.ToneSuccess)
						} else {
							components.Chip(c, "密钥未配置", components.ToneNeutral)
						}
						components.Chip(c, fmt.Sprintf("%d 个模型", provider.ModelCount), components.ToneNeutral)
					})
				})
			})

			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				label := "测试连接"
				if p.testingID == provider.ID {
					label = "测试中…"
				}
				if components.Button(c, components.ButtonProps{
					Label:    label,
					Variant:  components.ButtonSecondary,
					Disabled: p.testingID != "",
				}).Clicked() {
					p.testProvider(provider)
				}
				if components.Button(c, components.ButtonProps{
					Label:   "编辑",
					Variant: components.ButtonGhost,
				}).Clicked() {
					p.openProviderDialog(provider.ID)
				}
				if components.TextButton(c, "删除", true).Clicked() {
					p.deleteProviderID = provider.ID
					p.deleteProviderOpen = true
				}
			})
		})

		if result := p.testResult[provider.ID]; result != "" {
			color := t.Success
			if p.testFailed[provider.ID] {
				color = t.Danger
			}
			ui.Text(c, result).FontSize(11).TextColor(color)
		}
	})
}

func (p *Page) providerModels(c *ui.Context, provider contract.ProviderConfig) {
	t := c.Theme()

	items := make([]contract.ModelConfig, 0, len(p.modelList))
	for _, item := range p.modelList {
		if item.ProviderID == provider.ID {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return
	}

	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, provider.Name).FontSize(12).Bold().TextColor(t.TextMuted)
			ui.Text(c, fmt.Sprintf("%d 个模型", len(items))).FontSize(11).TextColor(t.TextMuted)
		})
		for _, item := range items {
			p.modelRow(c, item)
		}
	})
}

func (p *Page) modelRow(c *ui.Context, item contract.ModelConfig) {
	t := c.Theme()

	row := ui.Row(c).
		FillWidth().
		Padding(10, 12).
		Radius(theme.RadiusSM).
		BorderWidth(1).
		BorderColor(t.Border).
		Gap(10).
		AlignItems(ui.Center)

	row.Children(func() {
		ui.Column(c).Gap(3).Grow(1).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.Name).FontSize(13).Bold().MaxLines(1)
				if item.Key == p.defaultKey {
					components.Chip(c, "默认", components.ToneBrand)
				}
				if !item.Enabled {
					components.Chip(c, "已停用", components.ToneNeutral)
				}
			})

			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.ModelID).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
				if item.Vision {
					components.Chip(c, "图片理解", components.ToneBrand)
				}
				if item.ContextTokens > 0 {
					components.Chip(c, "上下文 "+tokenText(item.ContextTokens), components.ToneNeutral)
				}
				if item.MaxOutputTokens > 0 {
					components.Chip(c, "输出 "+tokenText(item.MaxOutputTokens), components.ToneNeutral)
				}
			})
		})

		ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
			if item.Key != p.defaultKey && item.Enabled {
				if components.TextButton(c, "设为默认", false).Clicked() {
					p.setDefaultModel(item.Key)
				}
			}
			if components.TextButton(c, "编辑", false).Clicked() {
				p.openModelDialog(item.ProviderID, item.Key)
			}
			if components.TextButton(c, "删除", true).Clicked() {
				p.deleteModelKey = item.Key
				p.deleteModelOpen = true
			}
		})
	})
}

func (p *Page) setDefaultModel(key string) {
	if p.models == nil {
		return
	}
	if err := p.models.SetDefaultModel(key); err != nil {
		p.setError(err.Error())
		return
	}
	p.reload()
	p.setNotice("默认模型已切换，重启后生效。")
}

// providerByID 返回服务商副本，找不到时返回 nil。
func (p *Page) providerByID(id string) *contract.ProviderConfig {
	for i := range p.providers {
		if p.providers[i].ID == id {
			return &p.providers[i]
		}
	}
	return nil
}

// providerOptions 把服务商转换为下拉选项。
func (p *Page) providerOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, len(p.providers))
	for _, item := range p.providers {
		desc := item.BaseURL
		if !item.Enabled {
			desc = "已停用"
		}
		out = append(out, components.SelectOption{
			Value:       item.ID,
			Label:       item.Name,
			Description: desc,
		})
	}
	return out
}

// alreadyAdded 判断某个上游模型是否已在该服务商下添加。
func (p *Page) alreadyAdded(providerID, modelID string) bool {
	key := providerID + "/" + strings.TrimSpace(modelID)
	for _, item := range p.modelList {
		if item.Key == key {
			return true
		}
	}
	return false
}

// tokenText 把 token 数格式化为 128K 这类简短文本。
func tokenText(n int) string {
	if n <= 0 {
		return "0"
	}
	if n%1000 != 0 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%dK", n/1000)
}

// update 把状态修改切回主线程；未注入钩子时直接执行。
func (p *Page) update(fn func()) {
	if p.hooks.Update != nil {
		p.hooks.Update(fn)
		return
	}
	fn()
}
