package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"bowknot-workbench/internal/app"
	"bowknot-workbench/internal/backend"
	"bowknot-workbench/internal/builtin"
	"bowknot-workbench/internal/infra/config"
	pkgs "bowknot-workbench/internal/infra/package"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/pet"
	"bowknot-workbench/internal/platform/lifecycle"
	"bowknot-workbench/internal/platform/logging"
	"bowknot-workbench/internal/platform/paths"
	"bowknot-workbench/internal/store"
	"bowknot-workbench/internal/tts"
	"bowknot-workbench/internal/ui/components"
	petui "bowknot-workbench/internal/ui/pet"
	"bowknot-workbench/internal/ui/shell"
	"bowknot-workbench/internal/ui/theme"

	"github.com/egoist/mygo"
)

const version = "0.0.1"

// desktopTray 保持托盘对象存活，避免被回收后图标消失。
var desktopTray *mygo.Tray

// petLines 是点击桌宠时播报的短句，用于验证语音链路。
var petLines = []string{
	"你好呀，我是蝴蝶结。",
	"今天也要开开心心的。",
	"需要我帮忙吗？",
}

// newTTSProvider 按当前配置创建豆包语音合成客户端。
func newTTSProvider(item config.TTS) *tts.Doubao {
	return tts.NewDoubao(tts.DoubaoOptions{
		BaseURL:    item.BaseURL,
		APIKey:     item.APIKey,
		ResourceID: item.ResourceID,
		Speaker:    item.Speaker,
		Format:     item.Format,
		SampleRate: item.SampleRate,
		SpeechRate: item.SpeechRate,
	})
}

func main() {
	dataPaths, err := paths.Resolve()
	if err != nil {
		slog.Error("解析数据目录失败", "error", err)
		os.Exit(1)
	}
	if err := dataPaths.Prepare(); err != nil {
		slog.Error("初始化数据目录失败", "error", err)
		os.Exit(1)
	}
	// 启动自检：提前暴露目录不可写的问题，避免运行中途才失败。
	if err := dataPaths.CheckWritable(); err != nil {
		slog.Error("启动自检失败", "error", err)
		os.Exit(1)
	}

	lc := lifecycle.New()

	cfg, err := config.Load(dataPaths.Config)
	if err != nil {
		slog.Error("加载配置失败", "error", err)
		os.Exit(1)
	}

	logger, closeLog, err := logging.New(logging.Options{
		Dir:   dataPaths.Logs,
		Level: logging.ParseLevel(cfg.LogLevel),
	})
	if err != nil {
		slog.Error("初始化日志失败", "error", err)
		os.Exit(1)
	}
	lc.Add("logging", func(context.Context) error { return closeLog() })
	slog.SetDefault(logger)

	logger.Info("应用启动", "version", version, "root", dataPaths.Root)

	db, err := store.Open(lc.Context(), store.Options{
		Path:   dataPaths.Database,
		Logger: logger,
		// 默认不导入演示会话，只有显式设置 BOWKNOT_SEED=1 的开发场景才写入。
		Seed: os.Getenv("BOWKNOT_SEED") == "1",
	})
	if err != nil {
		logger.Error("打开数据存储失败", "error", err)
		os.Exit(1)
	}
	lc.Add("store", func(ctx context.Context) error { return db.Close(ctx) })

	// 运行设置存放在 app_meta，保存后立即生效；主题在壳层初始化时读取。
	settingsSvc := app.NewSettingsService(db, logger)

	// 网关按配置的多个服务商与模型构建路由；没有任何可用服务商时回退演示实现。
	modelConfig := app.NewModelConfigService(dataPaths.Config, logger)
	provider := app.NewProviderFromConfig(cfg)
	chatModels := modelConfig.ChatModels()
	logger.Info("模型网关已初始化",
		"providers", len(cfg.Providers),
		"models", len(chatModels),
		"defaultModel", modelConfig.DefaultModel(),
	)
	// Agent 里可能还存着旧版模型 ID，启动时统一迁移为内部 key。
	app.ReconcileAgentModels(lc.Context(), db, chatModels, modelConfig.DefaultModel(), logger)

	// 插件进程执行器需要先于安装器创建：覆盖安装或卸载前要释放进程，
	// 否则 Windows 下可执行文件被占用会导致替换失败。
	pluginProcesses := tool.NewProcessExecutor(tool.ProcessOptions{
		Store:  db,
		Logger: logger,
	})
	lc.Add("plugin-process", pluginProcesses.Close)

	// 包安装器负责应用/插件包的落盘、升级与卸载；启动时清空上次异常退出
	// 留下的安装残留，安装内置包并重试未完成的旧版本清理。
	installer := pkgs.NewInstaller(pkgs.Options{
		Store:       db,
		AppsRoot:    dataPaths.Apps,
		PluginsRoot: dataPaths.Plugins,
		TempRoot:    dataPaths.Temp,
		Logger:      logger,
		StopProcess: func(kind, id string) {
			if kind == pkgs.KindPlugin {
				pluginProcesses.StopPlugin(id)
			}
		},
	})
	installer.CleanTemp()
	if err := builtin.EnsureInstalled(lc.Context(), db, installer, logger); err != nil {
		logger.Warn("内置包安装未完成，将在下次启动重试", "error", err)
	}
	// 文件系统插件已改为内置工具：清掉旧版随包安装的插件，避免同名能力重复暴露给模型。
	if legacy, err := db.Plugins.Get(lc.Context(), "file-system"); err == nil && legacy.SourceType == pkgs.SourceTypeBuiltin {
		if err := installer.Uninstall(lc.Context(), pkgs.KindPlugin, "file-system", true); err != nil {
			logger.Warn("移除旧版文件系统插件失败", "error", err)
		}
	}
	installer.SweepOldVersions(lc.Context())

	// 工具服务是应用/插件工具的唯一执行入口，启动时把已安装的工具同步进注册表。
	// 危险工具的审批通过 ApprovalHub 转发到 UI，用户确认后才真正执行。
	approvals := app.NewApprovalHub()
	// 产物服务统一托管 Agent 交付物：校验来源、落盘、写库与删除。
	artifacts := app.NewArtifactService(db, dataPaths.Root, dataPaths.Artifacts, logger)
	// 文件系统工具统一限制在工作区内，写入的文件会直接登记到文件库。
	workspaceFS := app.NewFSService(dataPaths.Workspace)
	// ToolExecutor 内的 Agent 能力在 AgentService 创建后注入，这里先保留引用。
	executor := app.NewToolExecutor(db, pluginProcesses)
	executor.SetArtifacts(artifacts)
	executor.SetFS(workspaceFS)
	toolSvc := tool.NewService(tool.Options{
		Store:    db,
		Logger:   logger,
		Approver: approvals.Approve,
		Executor: executor,
	})
	builtinTools := append([]tool.ToolSpec{tool.PersonaToolSpec()}, tool.AgentHubToolSpecs()...)
	builtinTools = append(builtinTools, tool.TaskToolSpecs()...)
	builtinTools = append(builtinTools, tool.ArtifactToolSpec())
	builtinTools = append(builtinTools, tool.MemoryToolSpecs()...)
	builtinTools = append(builtinTools, tool.FSToolSpecs()...)
	toolSvc.RegisterBuiltin(builtinTools...)
	if err := toolSvc.SyncApps(lc.Context()); err != nil {
		logger.Warn("同步应用工具失败", "error", err)
	}
	if err := toolSvc.SyncPlugins(lc.Context()); err != nil {
		logger.Warn("同步插件工具失败", "error", err)
	}

	assistant := app.NewAssistantService(db, logger, provider, toolSvc, approvals)
	agents := app.NewAgentService(db, logger, provider, toolSvc, approvals)
	assistant.SetModels(chatModels)
	agents.SetModels(chatModels)
	assistant.SetArtifacts(artifacts)
	agents.SetArtifacts(artifacts)
	// 群聊服务负责主助手与成员的 @ 编排、轮次上限与最终汇总。
	groups := app.NewGroupService(db, logger, agents)
	agents.SetGroups(groups)
	// 任务服务负责主助手派发的后台任务：排队、执行、取消、重试与汇报。
	tasks := app.NewTaskService(db, logger, provider, agents, assistant)
	executor.SetAgentHub(app.NewAgentHub(agents, tasks))
	// 长期记忆服务负责工具写入、检索与周期摘要；助手与 Agent 共用同一个实例。
	memory := app.NewMemoryService(db, logger, provider)
	assistant.SetMemory(memory)
	agents.SetMemory(memory)
	executor.SetMemory(memory)
	tasks.SetMemory(memory)
	// 运行参数统一从设置读取：未保存过时使用默认值，保存后即时生效。
	assistant.SetSettings(settingsSvc)
	agents.SetSettings(settingsSvc)
	groups.SetSettings(settingsSvc)
	tasks.SetSettings(settingsSvc)
	memory.SetSettings(settingsSvc)
	memory.Start()
	lc.Add("assistant", assistant.Shutdown)
	lc.Add("agents", agents.Shutdown)
	lc.Add("groups", groups.Shutdown)
	lc.Add("tasks", tasks.Shutdown)
	lc.Add("memory", memory.Shutdown)

	appBackend := backend.NewApp(version)
	plugins := app.NewPluginService(db, installer, toolSvc, logger)
	appCatalog := app.NewAppCatalog(db, installer, toolSvc, logger)
	desktop := shell.New(shell.Deps{
		App:          appBackend,
		Plugins:      plugins,
		Apps:         appCatalog,
		Assistant:    assistant,
		Agents:       agents,
		Tasks:        tasks,
		Artifacts:    artifacts,
		Memory:       memory,
		Models:       modelConfig,
		Settings:     settingsSvc,
		DataRoot:     dataPaths.Root,
		ConfigPath:   dataPaths.Config,
		DatabasePath: dataPaths.Database,
		LogsPath:     dataPaths.Logs,
	})

	// 桌宠窗口与主窗口同生命周期，退出时停掉帧调度。
	var desktopPet *petui.Pet
	lc.Add("pet", func(context.Context) error {
		if desktopPet != nil {
			desktopPet.Stop()
		}
		return nil
	})

	// 语音合成走豆包 seed-tts-2.0；未填 apiKey 时点击桌宠只做动作、不发声。
	speaker := tts.NewSpeaker(newTTSProvider(cfg.TTS))
	lc.Add("tts", func(context.Context) error {
		speaker.Stop()
		return nil
	})
	// 真正开始播放时才切到说话动作，避免合成等待期间空张嘴。
	speaker.SetOnStart(func() {
		if desktopPet == nil {
			return
		}
		desktopPet.Window().Update(func() {
			desktopPet.Play("talk")
		})
	})
	if cfg.TTS.APIKey == "" {
		logger.Info("语音合成未配置 apiKey，可在设置 > 语音合成中填写", "config", dataPaths.Config)
	}

	// mygo 在系统强制退出等场景可能直接结束进程，OnQuit 是最后可靠的清理时机。
	mygo.App.OnQuit(func() {
		if err := lc.Close(); err != nil {
			logger.Error("退出清理失败", "error", err)
		}
	})
	// 全部窗口关闭后保留进程，由托盘负责重新打开或退出。
	mygo.App.OnWindowAllClosed(func() {})

	// petLineIndex 只在主线程的点击回调里递增，无需加锁。
	petLineIndex := 0
	// petRound 标识最新一次点击，用于丢弃旧播报的收尾回调。
	petRound := 0
	mygo.App.WhenReady(func() {
		desktop.Start()
		// 窗口验证阶段先直接创建桌宠，后续由设置项控制开关。
		bundle, err := pet.Builtin("bowknot")
		if err != nil {
			logger.Error("加载桌宠素材失败", "error", err)
			return
		}
		// 位置从设置恢复；首次启动落在主屏工作区右下角。
		var position *petui.Position
		if saved := settingsSvc.Settings().Pet; saved.Saved {
			position = &petui.Position{X: saved.X, Y: saved.Y}
		}
		desktopPet, err = petui.New(petui.Options{
			Bundle:   bundle,
			Position: position,
			SavePosition: func(x, y float64) {
				if err := settingsSvc.SavePetPosition(x, y); err != nil {
					logger.Warn("保存桌宠位置失败", "error", err)
				}
			},
			OnClick: func() {
				// 点击反应先播，语音真正开始时再切到 talk。
				desktopPet.Play("click")
				// 设置页保存后立即生效：每次点击重新读取配置。
				current, loadErr := config.Load(dataPaths.Config)
				if loadErr != nil {
					logger.Warn("读取语音合成配置失败", "error", loadErr)
					return
				}
				if !current.TTS.Enabled || current.TTS.APIKey == "" {
					return
				}
				speaker.SetProvider(newTTSProvider(current.TTS))
				text := petLines[petLineIndex%len(petLines)]
				petLineIndex++
				petRound++
				round := petRound
				// 合成与播放放到后台，避免阻塞 UI 线程。
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := speaker.Speak(ctx, text); err != nil {
						logger.Warn("语音播报失败", "error", err)
					}
					if desktopPet == nil {
						return
					}
					// 只让最后一次播报负责收尾，避免连续点击时被旧播报切回 idle。
					desktopPet.Window().Update(func() {
						if round == petRound {
							desktopPet.Play("idle")
						}
					})
				}()
			},
		})
		if err != nil {
			logger.Error("创建桌宠窗口失败", "error", err)
		}

		// 托盘常驻：关闭主窗口后仍能重新打开，也负责真正退出。
		// 图标用品牌 logo（蝴蝶结），左键直接唤起主窗口，右键弹出菜单。
		trayIcon, iconErr := components.LogoPNG(64, theme.Brand)
		if iconErr != nil {
			logger.Warn("渲染托盘图标失败", "error", iconErr)
		}
		desktopTray, err = mygo.NewTray(mygo.TrayOptions{
			Icon:    trayIcon,
			ToolTip: "Bowknot Workbench",
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: "打开主窗口", Click: func(*mygo.MenuItem, *mygo.Window) {
					desktop.Show()
				}},
				{Label: "显示桌宠", Type: mygo.MenuItemCheckbox, Checked: true, Click: func(item *mygo.MenuItem, _ *mygo.Window) {
					if desktopPet == nil {
						return
					}
					if item.IsChecked() {
						desktopPet.Window().Show()
					} else {
						desktopPet.Window().Hide()
					}
				}},
				mygo.Separator(),
				{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) {
					desktop.Quit()
				}},
			}),
		})
		if err != nil {
			logger.Warn("创建托盘图标失败", "error", err)
			return
		}
		desktopTray.OnClick(func() {
			desktop.Show()
		})
	})
	if err := mygo.App.Run(); err != nil {
		logger.Error("应用运行失败", "error", err)
		if closeErr := lc.Close(); closeErr != nil {
			logger.Error("退出清理失败", "error", closeErr)
		}
		os.Exit(1)
	}

	if err := lc.Close(); err != nil {
		logger.Error("退出清理失败", "error", err)
	}
}
