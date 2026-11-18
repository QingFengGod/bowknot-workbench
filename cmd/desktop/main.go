package main

import (
	"context"
	"log/slog"
	"os"

	"bowknot-workbench/internal/app"
	"bowknot-workbench/internal/backend"
	"bowknot-workbench/internal/builtin"
	"bowknot-workbench/internal/infra/config"
	pkgs "bowknot-workbench/internal/infra/package"
	"bowknot-workbench/internal/infra/tool"
	"bowknot-workbench/internal/platform/lifecycle"
	"bowknot-workbench/internal/platform/logging"
	"bowknot-workbench/internal/platform/paths"
	"bowknot-workbench/internal/store"
	"bowknot-workbench/internal/ui/shell"

	"github.com/egoist/mygo"
)

const version = "0.0.1"

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
	installer.SweepOldVersions(lc.Context())

	// 工具服务是应用/插件工具的唯一执行入口，启动时把已安装的工具同步进注册表。
	// 危险工具的审批通过 ApprovalHub 转发到 UI，用户确认后才真正执行。
	approvals := app.NewApprovalHub()
	// 产物服务统一托管 Agent 交付物：校验来源、落盘、写库与删除。
	artifacts := app.NewArtifactService(db, dataPaths.Root, dataPaths.Artifacts, logger)
	// ToolExecutor 内的 Agent 能力在 AgentService 创建后注入，这里先保留引用。
	executor := app.NewToolExecutor(db, pluginProcesses)
	executor.SetArtifacts(artifacts)
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

	// mygo 在系统强制退出等场景可能直接结束进程，OnQuit 是最后可靠的清理时机。
	mygo.App.OnQuit(func() {
		if err := lc.Close(); err != nil {
			logger.Error("退出清理失败", "error", err)
		}
	})

	mygo.App.WhenReady(func() {
		desktop.Start()
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
