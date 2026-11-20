package settings

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/components"
	"bowknot-workbench/internal/ui/theme"
)

// Deps 是设置页依赖的服务与路径信息，由壳层注入。
type Deps struct {
	App      contract.AppService
	Models   contract.ModelConfigService
	Settings contract.SettingsService
	// 数据目录信息用于「数据与日志」与「关于」分类。
	DataRoot     string
	ConfigPath   string
	DatabasePath string
	LogsPath     string
}

// Hooks 是页面依赖的外部动作，由壳层注入。
type Hooks struct {
	// Update 在主线程修改页面状态并触发重绘，供异步拉取与测试回写。
	Update func(fn func())
	// ApplyTheme 让壳层立即切换外观模式，持久化仍由本页负责。
	ApplyTheme func(theme string)
}

// section 是设置页左侧的分类。
type section struct {
	ID    string
	Label string
	Desc  string
}

// 设置页分类顺序，与方案文档保持一致。
var sections = []section{
	{ID: "models", Label: "模型服务", Desc: "管理服务商与可用模型，保存后重启生效。"},
	{ID: "voice", Label: "语音合成", Desc: "豆包语音合成 2.0，配置后点击桌宠即可播报。"},
	{ID: "prompt", Label: "对话与提示词", Desc: "全局提示词会追加到所有 Agent 的系统提示词。"},
	{ID: "task", Label: "任务调度", Desc: "后台任务的并发、超时与重试策略。"},
	{ID: "group", Label: "群聊与工具", Desc: "群聊讨论轮次与工具调用轮次上限。"},
	{ID: "memory", Label: "记忆", Desc: "长期记忆的注入条数、预算与摘要频率。"},
	{ID: "appearance", Label: "外观", Desc: "选择跟随系统或固定的明暗主题。"},
	{ID: "data", Label: "数据与日志", Desc: "查看数据目录、占用与日志级别。"},
	{ID: "about", Label: "关于", Desc: "应用版本与运行路径。"},
}

// Page 是设置页：左侧分类、右侧表单，配置写入由各服务负责。
type Page struct {
	app      contract.AppService
	models   contract.ModelConfigService
	settings contract.SettingsService
	hooks    Hooks

	dataRoot     string
	configPath   string
	databasePath string
	logsPath     string

	logLevel string
	usage    []usageItem
	usageErr string

	current string
	draft   contract.Settings

	providers  []contract.ProviderConfig
	modelList  []contract.ModelConfig
	defaultKey string
	notice     string
	errText    string

	// 服务商编辑弹窗。
	providerOpen       bool
	providerDraft      contract.ProviderConfig
	providerErr        string
	deleteProviderOpen bool
	deleteProviderID   string

	// 模型编辑弹窗。
	modelOpen         bool
	modelEditing      bool
	modelDraft        contract.ModelConfig
	modelContext      string
	modelMaxOutput    string
	modelErr          string
	modelProviderOpen bool
	deleteModelOpen   bool
	deleteModelKey    string

	// 拉取模型弹窗。
	fetchOpen         bool
	fetchProviderID   string
	fetchProviderOpen bool
	fetching          bool
	fetchErr          string
	fetchItems        []contract.ChatModel
	fetchChecks       map[string]*bool

	// 连接测试状态，键为服务商 ID。
	testingID  string
	testResult map[string]string
	testFailed map[string]bool

	// 运行设置表单草稿，数字用字符串保存以便处理空输入。
	promptGlobal       string
	taskConcurrent     string
	taskTimeout        string
	taskAttempts       string
	taskRetry          string
	groupRounds        string
	groupToolRounds    string
	memoryItems        string
	memoryBudget       string
	memoryRecentDays   string
	memoryDigestHours  string
	memoryContextTasks string

	// 语音合成表单草稿，数字同样用字符串保存。
	tts         contract.TTSSettings
	ttsKeyInput string
	ttsRate     string
	ttsSpeed    string

	// 恢复默认确认弹窗。
	resetOpen    bool
	resetSection string
}

func New(deps Deps, hooks Hooks) *Page {
	return &Page{
		app:          deps.App,
		models:       deps.Models,
		settings:     deps.Settings,
		dataRoot:     deps.DataRoot,
		configPath:   deps.ConfigPath,
		databasePath: deps.DatabasePath,
		logsPath:     deps.LogsPath,
		hooks:        hooks,
		current:      "models",
		testResult:   map[string]string{},
		testFailed:   map[string]bool{},
		fetchChecks:  map[string]*bool{},
	}
}

// OnShow 在切换到设置页时重新读取配置快照与数据目录占用。
func (p *Page) OnShow() {
	p.reload()
	p.loadDataInfo()
}

func (p *Page) reload() {
	if p.models == nil {
		p.providers = nil
		p.modelList = nil
		p.defaultKey = ""
		return
	}
	p.providers = p.models.Providers()
	p.modelList = p.models.Models()
	p.defaultKey = p.models.DefaultModel()
	p.tts = p.models.TTS()
	p.fillTTSEditors()

	if p.settings != nil {
		p.draft = p.settings.Settings()
		p.fillSettingDrafts()
	}
}

func (p *Page) View(c *ui.Context) {
	t := c.Theme()

	root := ui.Column(c).
		FillWidth().
		FillHeight().
		Padding(t.Space(3)).
		Background(t.Surface).
		Radius(theme.Radius).
		Clip()
	theme.NormalShadow.Apply(c, root)

	root.Children(func() {
		// Row 默认垂直居中，设置内容需要从顶部开始排列。
		ui.Row(c).FillWidth().Grow(1).Gap(t.Space(3)).AlignItems(ui.Start).Children(func() {
			p.sidebar(c)
			p.content(c)
		})
	})

	p.providerDialog(c)
	p.modelDialog(c)
	p.fetchDialog(c)
	p.deleteModals(c)
	p.resetModal(c)
}

func (p *Page) sidebar(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).
		Width(180).
		FillHeight().
		Shrink(0).
		Gap(2).
		Padding(0, t.Space(3), 0, 0).
		BorderWidth(0, 1, 0, 0).
		BorderColor(t.Border).
		Children(func() {
			ui.Text(c, "设置").FontSize(18).Bold().Padding(6, 10, 12, 10)

			for _, item := range sections {
				selected := item.ID == p.current
				btn := ui.ButtonBase(c).
					FillWidth().
					Height(34).
					PaddingX(10).
					Radius(theme.RadiusSM).
					AlignItems(ui.Center).
					Justify(ui.Start)
				if selected {
					btn.Background(t.Accent.Alpha(0.12))
				} else if btn.Hovered() {
					btn.Background(t.SurfaceHover)
				}
				btn.Children(func() {
					color := t.Text
					if selected {
						color = t.Accent
					}
					ui.Text(c, item.Label).FontSize(13).TextColor(color).MaxLines(1)
				})
				if btn.Clicked() && !selected {
					p.current = item.ID
					p.notice = ""
					p.errText = ""
				}
			}
		})
}

func (p *Page) content(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).FillHeight().Children(func() {
		ui.Column(c).FillWidth().Gap(t.Space(3)).Children(func() {
			meta := sections[0]
			for _, item := range sections {
				if item.ID == p.current {
					meta = item
					break
				}
			}

			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, meta.Label).FontSize(16).Bold()
				ui.Text(c, meta.Desc).FontSize(12).TextColor(t.TextMuted)
			})

			p.feedback(c)

			switch p.current {
			case "models":
				p.modelsSection(c)
			case "voice":
				p.voiceSection(c)
			case "prompt":
				p.promptSection(c)
			case "task":
				p.taskSection(c)
			case "group":
				p.groupSection(c)
			case "memory":
				p.memorySection(c)
			case "appearance":
				p.appearanceSection(c)
			case "data":
				p.dataSection(c)
			case "about":
				p.aboutSection(c)
			default:
				components.Card(c, components.CardProps{}, func() {
					ui.Text(c, "该分类正在建设中。").FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, "配置项将在后续版本开放，当前不会影响已有功能。").FontSize(12).TextColor(t.TextMuted)
				})
			}
		})
	})
}

func (p *Page) feedback(c *ui.Context) {
	t := c.Theme()

	if p.errText != "" {
		ui.Text(c, p.errText).FontSize(12).TextColor(t.Danger)
	}
	if p.notice != "" {
		ui.Text(c, p.notice).FontSize(12).TextColor(t.Success)
	}
}

func (p *Page) setNotice(text string) {
	p.notice = text
	p.errText = ""
}

func (p *Page) setError(text string) {
	p.errText = text
	p.notice = ""
}
