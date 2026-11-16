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
	"bowknot-workbench/internal/infra/llm"
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

	// 网关按配置选择实现：baseUrl 为空时回退到演示用假实现；
	// 密钥只进入网关，日志里输出脱敏后的值。
	provider := llm.New(llm.Options{
		Provider:     cfg.Model.Provider,
		BaseURL:      cfg.Model.BaseURL,
		APIKey:       cfg.Model.APIKey,
		DefaultModel: cfg.Model.DefaultModel,
	})
	logger.Info("模型网关已初始化",
		"provider", cfg.Model.Provider,
		"baseURL", cfg.Model.BaseURL,
		"apiKey", config.MaskKey(cfg.Model.APIKey),
	)

	// 启动时向网关查询一次模型列表并缓存，避免 UI 每次进入页面都请求外部服务；
	// 超时或查询失败时退回配置的默认模型。
	modelCtx, cancelModels := context.WithTimeout(lc.Context(), 10*time.Second)
	models := app.ResolveModels(modelCtx, provider, cfg.Model.DefaultModel, logger)
	cancelModels()
	logger.Info("模型列表已缓存", "count", len(models))

	// 包安装器负责应用/插件包的落盘、升级与卸载；启动时清空上次异常退出
	// 留下的安装残留，安装内置包并重试未完成的旧版本清理。
	installer := pkgs.NewInstaller(pkgs.Options{
		Store:       db,
		AppsRoot:    dataPaths.Apps,
		PluginsRoot: dataPaths.Plugins,
		TempRoot:    dataPaths.Temp,
		Logger:      logger,
	})
	installer.CleanTemp()
	if err := builtin.EnsureInstalled(lc.Context(), db, installer, logger); err != nil {
		logger.Warn("内置包安装未完成，将在下次启动重试", "error", err)
	}
	installer.SweepOldVersions(lc.Context())

	// 工具服务是应用/插件工具的唯一执行入口，启动时把已安装的工具同步进注册表。
	// 危险工具的审批通过 ApprovalHub 转发到 UI，用户确认后才真正执行。
	approvals := app.NewApprovalHub()
	toolSvc := tool.NewService(tool.Options{
		Store:    db,
		Logger:   logger,
		Approver: approvals.Approve,
		Executor: app.NewToolExecutor(db),
	})
	toolSvc.RegisterBuiltin(tool.PersonaToolSpec())
	if err := toolSvc.SyncApps(lc.Context()); err != nil {
		logger.Warn("同步应用工具失败", "error", err)
	}
	if err := toolSvc.SyncPlugins(lc.Context()); err != nil {
		logger.Warn("同步插件工具失败", "error", err)
	}

	assistant := app.NewAssistantService(db, logger, provider, toolSvc, approvals)
	agents := app.NewAgentService(db, logger, provider, toolSvc, approvals)
	assistant.SetModels(models)
	agents.SetModels(models)
	// 长期记忆服务负责抽取、检索与摘要；助手与 Agent 共用同一个实例。
	memory := app.NewMemoryService(db, logger, provider)
	assistant.SetMemory(memory)
	agents.SetMemory(memory)
	lc.Add("assistant", assistant.Shutdown)
	lc.Add("agents", agents.Shutdown)
	lc.Add("memory", memory.Shutdown)

	appBackend := backend.NewApp(version)
	plugins := app.NewPluginService(db, installer, toolSvc, logger)
	appCatalog := app.NewAppCatalog(db, installer, toolSvc, logger)
	desktop := shell.New(shell.Deps{
		App:       appBackend,
		Plugins:   plugins,
		Apps:      appCatalog,
		Assistant: assistant,
		Agents:    agents,
		Memory:    memory,
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
