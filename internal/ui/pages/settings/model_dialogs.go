package settings

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/theme"
)

// ---------- 服务商 ----------

func (p *Page) openProviderDialog(id string) {
	p.providerErr = ""
	if id == "" {
		p.providerDraft = contract.ProviderConfig{Type: "openai-compatible", Enabled: true}
	} else {
		draft := contract.ProviderConfig{ID: id, Type: "openai-compatible", Enabled: true}
		if item := p.providerByID(id); item != nil {
			draft = *item
			draft.APIKeyInput = ""
			draft.ClearKey = false
		}
		p.providerDraft = draft
	}
	p.providerOpen = true
}

func (p *Page) providerDialog(c *ui.Context) {
	t := c.Theme()

	title := "添加服务商"
	if p.providerDraft.ID != "" {
		title = "编辑服务商"
	}

	save := func() {
		if p.models == nil {
			return
		}
		draft := p.providerDraft
		draft.Name = strings.TrimSpace(draft.Name)
		draft.BaseURL = strings.TrimSpace(draft.BaseURL)
		draft.APIKeyInput = strings.TrimSpace(draft.APIKeyInput)
		if draft.Name == "" {
			p.providerErr = "服务商名称不能为空"
			return
		}
		providerID, err := p.models.SaveProvider(draft)
		if err != nil {
			p.providerErr = err.Error()
			return
		}
		delete(p.testResult, providerID)
		delete(p.testFailed, providerID)
		p.providerOpen = false
		p.reload()
		p.setNotice("服务商已保存，重启后生效。")
	}

	components.Modal(c, components.ModalProps{
		Open:  &p.providerOpen,
		Title: title,
		Width: 480,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.providerOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:   "保存",
				Variant: components.ButtonPrimary,
			}).Clicked() {
				save()
			}
		},
	}, func() {
		ui.Text(c, "名称").FontSize(12).Bold().TextColor(t.TextMuted)
		nameInput := ui.TextInput(c, &p.providerDraft.Name).
			Placeholder("例如：OpenAI").
			MaxLength(40)
		if nameInput.Changed() {
			p.providerErr = ""
		}

		ui.Text(c, "BaseURL").FontSize(12).Bold().TextColor(t.TextMuted)
		ui.TextInput(c, &p.providerDraft.BaseURL).
			Placeholder("https://api.openai.com/v1").
			MaxLength(200)

		ui.Text(c, "API Key").FontSize(12).Bold().TextColor(t.TextMuted)
		placeholder := "输入 API Key"
		if p.providerDraft.HasKey {
			placeholder = "已保存 " + p.providerDraft.APIKey + "，留空保持不变"
		}
		keyInput := ui.TextInput(c, &p.providerDraft.APIKeyInput).
			Password().
			Placeholder(placeholder).
			MaxLength(200)

		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c, &p.providerDraft.Enabled, "启用该服务商")
			if p.providerDraft.ID != "" && p.providerDraft.HasKey {
				ui.Checkbox(c, &p.providerDraft.ClearKey, "清除已保存的密钥")
			}
		})

		ui.Text(c, "所有服务商统一按 OpenAI 兼容协议访问，修改后需要重启应用生效。").
			FontSize(11).
			TextColor(t.TextMuted)

		if p.providerErr != "" {
			ui.Text(c, p.providerErr).FontSize(12).TextColor(t.Danger)
		}

		if (nameInput.Submitted() || keyInput.Submitted()) && p.providerErr == "" {
			save()
		}
	})
}

func (p *Page) testProvider(provider contract.ProviderConfig) {
	if p.models == nil || p.testingID != "" {
		return
	}
	svc := p.models
	draft := provider
	p.testingID = provider.ID
	delete(p.testResult, provider.ID)
	delete(p.testFailed, provider.ID)

	go func() {
		message, err := svc.TestProvider(draft)
		p.update(func() {
			p.testingID = ""
			if err != nil {
				p.testFailed[provider.ID] = true
				p.testResult[provider.ID] = err.Error()
				return
			}
			p.testFailed[provider.ID] = false
			p.testResult[provider.ID] = message
		})
	}()
}

// ---------- 模型 ----------

func (p *Page) openModelDialog(providerID, key string) {
	p.modelErr = ""

	if key != "" {
		for _, item := range p.modelList {
			if item.Key != key {
				continue
			}
			p.modelEditing = true
			p.modelDraft = item
			p.modelContext = intText(item.ContextTokens)
			p.modelMaxOutput = intText(item.MaxOutputTokens)
			p.modelOpen = true
			return
		}
	}

	if providerID == "" && len(p.providers) > 0 {
		providerID = p.providers[0].ID
	}
	if providerID == "" {
		p.setError("请先添加服务商，再录入模型。")
		return
	}
	p.modelEditing = false
	p.modelDraft = contract.ModelConfig{ProviderID: providerID, Enabled: true}
	p.modelContext = ""
	p.modelMaxOutput = ""
	p.modelOpen = true
}

func (p *Page) modelDialog(c *ui.Context) {
	t := c.Theme()

	title := "添加模型"
	if p.modelEditing {
		title = "编辑模型"
	}

	save := func() {
		if p.models == nil {
			return
		}
		draft := p.modelDraft
		draft.ProviderID = strings.TrimSpace(draft.ProviderID)
		draft.ModelID = strings.TrimSpace(draft.ModelID)
		draft.Name = strings.TrimSpace(draft.Name)
		if draft.ProviderID == "" {
			p.modelErr = "请选择服务商"
			return
		}
		if draft.ModelID == "" {
			p.modelErr = "模型 ID 不能为空"
			return
		}
		if strings.Contains(draft.ModelID, "/") {
			p.modelErr = "模型 ID 不能包含斜杠"
			return
		}

		contextTokens, err := parseOptionalInt(p.modelContext)
		if err != nil {
			p.modelErr = "上下文长度" + err.Error()
			return
		}
		maxOutput, err := parseOptionalInt(p.modelMaxOutput)
		if err != nil {
			p.modelErr = "最大输出长度" + err.Error()
			return
		}
		if contextTokens != 0 && contextTokens < 1000 {
			p.modelErr = "上下文长度需不少于 1000，或留空表示未填写"
			return
		}
		if maxOutput > 1000000 {
			p.modelErr = "最大输出长度不能超过 1,000,000"
			return
		}
		if contextTokens != 0 && maxOutput > contextTokens {
			p.modelErr = "最大输出长度不能大于上下文长度"
			return
		}

		draft.ContextTokens = contextTokens
		draft.MaxOutputTokens = maxOutput
		if err := p.models.SaveModel(draft); err != nil {
			p.modelErr = err.Error()
			return
		}
		p.modelOpen = false
		p.reload()
		p.setNotice("模型已保存，重启后生效。")
	}

	components.Modal(c, components.ModalProps{
		Open:  &p.modelOpen,
		Title: title,
		Width: 500,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "取消",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.modelOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:   "保存",
				Variant: components.ButtonPrimary,
			}).Clicked() {
				save()
			}
		},
	}, func() {
		ui.Text(c, "服务商").FontSize(12).Bold().TextColor(t.TextMuted)
		if p.modelEditing {
			name := p.modelDraft.ProviderID
			if provider := p.providerByID(p.modelDraft.ProviderID); provider != nil {
				name = provider.Name
			}
			ui.Box(c).
				FillWidth().
				Padding(8, 10).
				Radius(theme.RadiusSM).
				BorderWidth(1).
				BorderColor(t.Border).
				Background(t.Background).
				Children(func() {
					ui.Text(c, name).FontSize(13).TextColor(t.TextMuted)
				})
		} else {
			components.SelectField(c, components.SelectFieldProps{
				Value:       p.modelDraft.ProviderID,
				Placeholder: "请选择服务商",
				Options:     p.providerOptions(),
				Open:        &p.modelProviderOpen,
				OnSelect: func(value string) {
					p.modelDraft.ProviderID = value
					p.modelErr = ""
				},
			})
		}

		ui.Text(c, "上游模型 ID").FontSize(12).Bold().TextColor(t.TextMuted)
		if p.modelEditing {
			ui.Box(c).
				FillWidth().
				Padding(8, 10).
				Radius(theme.RadiusSM).
				BorderWidth(1).
				BorderColor(t.Border).
				Background(t.Background).
				Children(func() {
					ui.Text(c, p.modelDraft.ModelID).FontSize(13).TextColor(t.TextMuted)
				})
		} else {
			idInput := ui.TextInput(c, &p.modelDraft.ModelID).
				Placeholder("例如：gpt-4o，不能包含斜杠").
				MaxLength(100)
			if idInput.Changed() {
				p.modelErr = ""
			}
		}

		ui.Text(c, "显示名称").FontSize(12).Bold().TextColor(t.TextMuted)
		ui.TextInput(c, &p.modelDraft.Name).
			Placeholder("留空时使用上游模型 ID").
			MaxLength(60)

		ui.Checkbox(c, &p.modelDraft.Vision, "支持图片理解")

		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "上下文长度").FontSize(12).Bold().TextColor(t.TextMuted)
				ui.TextInput(c, &p.modelContext).Placeholder("例如：128000").MaxLength(12)
			})
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "最大输出长度").FontSize(12).Bold().TextColor(t.TextMuted)
				ui.TextInput(c, &p.modelMaxOutput).Placeholder("例如：16384").MaxLength(12)
			})
		})

		ui.Checkbox(c, &p.modelDraft.Enabled, "启用该模型")

		ui.Text(c, "图片理解、上下文与最大输出本期仅保存并展示，尚未参与请求。").
			FontSize(11).
			TextColor(t.TextMuted)

		if p.modelErr != "" {
			ui.Text(c, p.modelErr).FontSize(12).TextColor(t.Danger)
		}
	})
}

// ---------- 拉取模型 ----------

func (p *Page) openFetchDialog() {
	p.fetchErr = ""
	p.fetchItems = nil
	p.fetchChecks = map[string]*bool{}
	if len(p.providers) == 0 {
		p.setError("请先添加服务商，再拉取模型。")
		return
	}
	p.fetchProviderID = p.providers[0].ID
	for _, item := range p.providers {
		if item.Enabled {
			p.fetchProviderID = item.ID
			break
		}
	}
	p.fetchOpen = true
}

func (p *Page) startFetch() {
	if p.models == nil || p.fetching || p.fetchProviderID == "" {
		return
	}
	svc := p.models
	providerID := p.fetchProviderID
	p.fetching = true
	p.fetchErr = ""
	p.fetchItems = nil
	p.fetchChecks = map[string]*bool{}

	go func() {
		items, err := svc.FetchModels(providerID)
		p.update(func() {
			p.fetching = false
			if err != nil {
				p.fetchErr = err.Error()
				return
			}
			p.fetchItems = items
			p.fetchChecks = make(map[string]*bool, len(items))
			for _, item := range items {
				checked := false
				p.fetchChecks[item.ID] = &checked
			}
			if len(items) == 0 {
				p.fetchErr = "上游没有返回可用模型"
			}
		})
	}()
}

func (p *Page) fetchDialog(c *ui.Context) {
	t := c.Theme()

	addCount := 0
	for _, item := range p.fetchItems {
		checked := p.fetchChecks[item.ID]
		if checked != nil && *checked && !p.alreadyAdded(p.fetchProviderID, item.ID) {
			addCount++
		}
	}

	components.Modal(c, components.ModalProps{
		Open:  &p.fetchOpen,
		Title: "拉取模型",
		Width: 560,
		Footer: func() {
			if components.Button(c, components.ButtonProps{
				Label:   "关闭",
				Variant: components.ButtonSecondary,
			}).Clicked() {
				p.fetchOpen = false
			}
			if components.Button(c, components.ButtonProps{
				Label:    fmt.Sprintf("添加所选（%d）", addCount),
				Variant:  components.ButtonPrimary,
				Disabled: addCount == 0 || p.fetching,
			}).Clicked() {
				p.addFetchedModels()
			}
		},
	}, func() {
		ui.Text(c, "服务商").FontSize(12).Bold().TextColor(t.TextMuted)
		components.SelectField(c, components.SelectFieldProps{
			Value:       p.fetchProviderID,
			Placeholder: "请选择服务商",
			Options:     p.providerOptions(),
			Open:        &p.fetchProviderOpen,
			OnSelect: func(value string) {
				if value == p.fetchProviderID {
					return
				}
				p.fetchProviderID = value
				p.fetchItems = nil
				p.fetchChecks = map[string]*bool{}
				p.fetchErr = ""
			},
		})

		label := "开始拉取"
		if p.fetching {
			label = "拉取中…"
		}
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			if components.Button(c, components.ButtonProps{
				Label:    label,
				Variant:  components.ButtonSecondary,
				Disabled: p.fetching,
			}).Clicked() {
				p.startFetch()
			}
			if !p.fetching && len(p.fetchItems) > 0 {
				ui.Text(c, fmt.Sprintf("远端共 %d 个模型", len(p.fetchItems))).
					FontSize(11).
					TextColor(t.TextMuted)
			}
		})

		if p.fetchErr != "" {
			ui.Text(c, p.fetchErr).FontSize(12).TextColor(t.Danger)
		}

		if len(p.fetchItems) == 0 {
			return
		}

		ui.Scroll(c).FillWidth().Height(300).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for _, item := range p.fetchItems {
					p.fetchRow(c, item)
				}
			})
		})
	})
}

func (p *Page) fetchRow(c *ui.Context, item contract.ChatModel) {
	t := c.Theme()

	added := p.alreadyAdded(p.fetchProviderID, item.ID)
	invalid := strings.Contains(item.ID, "/")

	row := ui.Row(c).
		FillWidth().
		Padding(6, 8).
		Radius(theme.RadiusSM).
		Gap(10).
		AlignItems(ui.Center)
	if !added && !invalid && row.Hovered() {
		row.Background(t.SurfaceHover)
	}

	row.Children(func() {
		if added || invalid {
			ui.Box(c).Width(16).Height(16).Shrink(0)
		} else {
			checked := p.fetchChecks[item.ID]
			if checked == nil {
				checked = boolPointer(false)
				p.fetchChecks[item.ID] = checked
			}
			ui.Checkbox(c, checked, "")
		}

		ui.Column(c).Gap(2).Grow(1).Children(func() {
			name := item.Name
			if name == "" {
				name = item.ID
			}
			ui.Text(c, name).FontSize(13).MaxLines(1)
			ui.Text(c, item.ID).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		})

		switch {
		case added:
			components.Chip(c, "已添加", components.ToneNeutral)
		case invalid:
			components.Chip(c, "ID 含斜杠，暂不支持", components.ToneDanger)
		}
	})
}

func (p *Page) addFetchedModels() {
	if p.models == nil {
		return
	}

	added := 0
	failure := ""
	for _, item := range p.fetchItems {
		checked := p.fetchChecks[item.ID]
		if checked == nil || !*checked {
			continue
		}
		if p.alreadyAdded(p.fetchProviderID, item.ID) {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.ID
		}
		err := p.models.SaveModel(contract.ModelConfig{
			ProviderID: p.fetchProviderID,
			ModelID:    item.ID,
			Name:       name,
			Enabled:    true,
		})
		if err != nil {
			failure = err.Error()
			continue
		}
		added++
	}

	p.reload()
	if failure != "" {
		p.fetchErr = failure
		return
	}
	p.fetchOpen = false
	if added == 0 {
		p.setNotice("所选模型都已添加过。")
		return
	}
	p.setNotice(fmt.Sprintf("已添加 %d 个模型，重启后生效。", added))
}

// ---------- 删除确认 ----------

func (p *Page) deleteModals(c *ui.Context) {
	if p.deleteProviderOpen {
		id := p.deleteProviderID
		if ui.AlertDialog(c, &p.deleteProviderOpen,
			"删除服务商",
			"删除后无法恢复；服务商下仍有模型时需要先删除模型。",
			"取消", "删除") == 1 {
			if p.models != nil {
				if err := p.models.DeleteProvider(id); err != nil {
					p.setError(err.Error())
				} else {
					delete(p.testResult, id)
					delete(p.testFailed, id)
					p.reload()
					p.setNotice("服务商已删除，重启后生效。")
				}
			}
		}
		if !p.deleteProviderOpen {
			p.deleteProviderID = ""
		}
	}

	if p.deleteModelOpen {
		key := p.deleteModelKey
		if ui.AlertDialog(c, &p.deleteModelOpen,
			"删除模型",
			"删除后无法恢复；若它是默认模型，将自动切换到其它启用模型。",
			"取消", "删除") == 1 {
			if p.models != nil {
				if err := p.models.DeleteModel(key); err != nil {
					p.setError(err.Error())
				} else {
					p.reload()
					p.setNotice("模型已删除，重启后生效。")
				}
			}
		}
		if !p.deleteModelOpen {
			p.deleteModelKey = ""
		}
	}
}

// ---------- 工具 ----------

func parseOptionalInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, errors.New("需填写不小于 0 的整数")
	}
	return n, nil
}

func intText(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func boolPointer(value bool) *bool {
	return &value
}
