package contract

// AppInfo 描述当前工作台的版本信息。
type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// AppService 是 UI 层可以依赖的应用元信息接口。
// 该包不允许依赖 MyGo 或任何具体 UI 实现。
type AppService interface {
	Info() AppInfo
}
