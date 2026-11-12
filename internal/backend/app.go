package backend

import "bowknot-workbench/internal/contract"

// App 实现 contract.AppService，不依赖 UI 或 MyGo。
type App struct {
	version string
}

func NewApp(version string) *App {
	return &App{version: version}
}

func (a *App) Info() contract.AppInfo {
	return contract.AppInfo{
		Name:    "Bowknot Workbench",
		Version: a.version,
	}
}
