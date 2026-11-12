package components

import "github.com/egoist/mygo/ui"

// IconName 是原生界面可用的图标名。
type IconName string

const (
	IconLogo      IconName = "logo"
	IconWorkbench IconName = "workbench"
	IconApps      IconName = "apps"
	IconAssistant IconName = "assistant"
	IconAgents    IconName = "agents"
	IconPlugins   IconName = "plugins"
	IconAIGC      IconName = "aigc"
	IconPlus      IconName = "plus"
	IconTrash     IconName = "trash"
	IconClose     IconName = "close"
	IconSearch    IconName = "search"
	IconEdit      IconName = "edit"
	IconSend      IconName = "send"
	IconStop      IconName = "stop"
	IconPanel     IconName = "panel"
	IconMessage   IconName = "message"
	IconMore      IconName = "more"
	IconChevron   IconName = "chevron"
	IconSparkles  IconName = "sparkles"
	IconImage     IconName = "image"
	IconUser      IconName = "user"
	IconFileText  IconName = "fileText"
	IconDashboard IconName = "dashboard"
	IconPerson    IconName = "person"
	IconTasks     IconName = "tasks"
	IconSun       IconName = "sun"
	IconMoon      IconName = "moon"
	IconMonitor   IconName = "monitor"
)

// 图标统一为 lucide 风格 SVG（24 viewBox、stroke 2），保持与原生 UI 一致的线条观感。
var icons = map[IconName]*ui.SVG{
	IconLogo:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512"><path fill="currentColor" d="M447.586 34.278c8.2.042 16.652.51 25.25 1.305c-18.81 11.825-47.438 25.416-75.809 35.345c-19.621 6.868-39.233 12.234-55.464 14.86c-10 1.617-18.485 1.793-24.758 1.267c9.46-16.636 28.867-31.502 56.191-41.146c20.184-7.124 44.389-11.352 71.092-11.623a226 226 0 0 1 3.498-.008m-383.172 0a226 226 0 0 1 3.498.008c26.703.271 50.908 4.5 71.092 11.623c27.324 9.644 46.73 24.51 56.191 41.146c-6.273.526-14.758.35-24.757-1.267c-16.232-2.626-35.844-7.992-55.465-14.86c-28.37-9.93-56.998-23.52-75.809-35.345c8.598-.794 17.05-1.263 25.25-1.305m417.828 17.5c-10.731 63.387-9.774 151.122 2.854 210.18c-35.275 4.762-73.838-2.481-105.834-14.924c-17.386-6.761-32.812-14.983-44.6-23.235c-4.285-3-8.02-6.019-11.203-8.923a63 63 0 0 0 3.34-3.557c14.844-17.115 17.859-41.615 14.031-64.781c-2.39-14.463-7.447-28.663-14.908-40.76c24.99.44 54.528-7.35 83.19-17.91c27.592-10.166 53.726-23.013 73.13-36.09m-452.484 0c19.404 13.077 45.538 25.924 73.13 36.09c28.662 10.56 58.2 18.35 83.19 17.91c-7.461 12.097-12.519 26.297-14.908 40.76c-3.827 23.166-.813 47.666 14.031 64.781a63 63 0 0 0 3.34 3.557c-3.183 2.904-6.918 5.924-11.203 8.923c-11.788 8.252-27.214 16.474-44.6 23.235c-31.996 12.443-70.559 19.686-105.834 14.924C39.532 202.9 40.49 115.165 29.758 51.778M256 86.698c16.205 0 32.227 4.958 42.104 13.523c12.056 10.457 21.739 29.716 24.966 49.25c3.228 19.535-.004 38.68-9.869 50.055c-11.469 13.224-34.123 21.07-57.201 21.07s-45.732-7.846-57.201-21.07c-9.865-11.375-13.097-30.52-9.87-50.055c3.228-19.534 12.91-38.793 24.967-49.25c9.877-8.565 25.9-13.523 42.104-13.523m52.7 139.254c4.475 4.282 9.729 8.457 15.638 12.594c13.212 9.248 29.786 18.026 48.4 25.265c1.586.617 3.192 1.219 4.807 1.813c20.777 65.205 52.5 128.055 85.127 183.976c-8.456-1.898-18.948-4.765-29.596-8.636c-21.259-7.731-44.304-19.496-58.713-33.905l-11.398-11.398l-3.72 15.683c-7.566 31.88-22.094 48.17-37.737 66.381c-35.313-65.017-51.22-150.543-55.283-239.533c15.155-1.202 29.988-5.135 42.474-12.24zm-105.4 0c12.487 7.105 27.32 11.038 42.475 12.24c-4.062 88.99-19.97 174.516-55.283 239.533c-15.643-18.21-30.17-34.502-37.736-66.38l-3.72-15.684l-11.4 11.398c-14.408 14.41-37.453 26.174-58.712 33.905c-10.648 3.871-21.14 6.738-29.596 8.636c32.626-55.92 64.35-118.771 85.127-183.976q2.425-.89 4.807-1.813c18.614-7.239 35.188-16.017 48.4-25.265c5.91-4.137 11.163-8.312 15.639-12.594z"/></svg>`),
	IconWorkbench: icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48"><g fill="none" stroke="currentColor" stroke-linejoin="round" stroke-width="4"><path d="M12 33H4V7h40v26z"/><path stroke-linecap="round" d="M16 22v4m8 7v6m0-21v8m8-12v12M12 41h24"/></g></svg>`),
	IconApps:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="M9 11.25H6A2.25 2.25 0 0 1 3.75 9V6A2.25 2.25 0 0 1 6 3.75h3A2.25 2.25 0 0 1 11.25 6v3A2.25 2.25 0 0 1 9 11.25m-3-6a.76.76 0 0 0-.75.75v3a.76.76 0 0 0 .75.75h3A.76.76 0 0 0 9.75 9V6A.76.76 0 0 0 9 5.25Zm3 15H6A2.25 2.25 0 0 1 3.75 18v-3A2.25 2.25 0 0 1 6 12.75h3A2.25 2.25 0 0 1 11.25 15v3A2.25 2.25 0 0 1 9 20.25m-3-6a.76.76 0 0 0-.75.75v3a.76.76 0 0 0 .75.75h3a.76.76 0 0 0 .75-.75v-3a.76.76 0 0 0-.75-.75Zm12-3h-3A2.25 2.25 0 0 1 12.75 9V6A2.25 2.25 0 0 1 15 3.75h3A2.25 2.25 0 0 1 20.25 6v3A2.25 2.25 0 0 1 18 11.25m-3-6a.76.76 0 0 0-.75.75v3a.76.76 0 0 0 .75.75h3a.76.76 0 0 0 .75-.75V6a.76.76 0 0 0-.75-.75Zm3 15h-3A2.25 2.25 0 0 1 12.75 18v-3A2.25 2.25 0 0 1 15 12.75h3A2.25 2.25 0 0 1 20.25 15v3A2.25 2.25 0 0 1 18 20.25m-3-6a.76.76 0 0 0-.75.75v3a.76.76 0 0 0 .75.75h3a.76.76 0 0 0 .75-.75v-3a.76.76 0 0 0-.75-.75Z"/></svg>`),
	IconAssistant: icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="M13.43 14.5a4.75 4.75 0 0 1 4.59 3.52l1.015 3.786l-1.45.388l-1.014-3.785A3.25 3.25 0 0 0 13.431 16H7.57a3.25 3.25 0 0 0-3.141 2.41l-1.015 3.784l-1.45-.388L2.98 18.02l.056-.193A4.75 4.75 0 0 1 7.57 14.5zM20.25 9c0 1.52 1.23 2.75 2.75 2.75v1.5c-1.52 0-2.75 1.23-2.75 2.75h-1.5c0-1.52-1.23-2.75-2.75-2.75v-1.5c1.52 0 2.75-1.23 2.75-2.75zm-.75 2.411a4.3 4.3 0 0 1-1.089 1.089c.426.294.795.662 1.089 1.088a4.3 4.3 0 0 1 1.088-1.088a4.3 4.3 0 0 1-1.088-1.089M10.5 2a5.5 5.5 0 1 1 0 11a5.5 5.5 0 0 1 0-11m0 1.5a4 4 0 1 0 0 8a4 4 0 0 0 0-8"/></svg>`),
	IconAgents:    icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="M17.5 14.25a3.24 3.24 0 0 0-2.62 1.35L9.59 13a3.4 3.4 0 0 0 .16-1a3.4 3.4 0 0 0-.16-1l5.29-2.6a3.23 3.23 0 1 0-.63-1.9a3 3 0 0 0 .05.5L8.83 9.75a3.2 3.2 0 0 0-2.33-1a3.25 3.25 0 0 0 0 6.5a3.2 3.2 0 0 0 2.33-1L14.3 17a3 3 0 0 0-.05.51a3.25 3.25 0 1 0 3.25-3.25Zm0-9.5a1.75 1.75 0 1 1-1.75 1.75a1.76 1.76 0 0 1 1.75-1.75m-11 9A1.75 1.75 0 1 1 8.25 12a1.76 1.76 0 0 1-1.75 1.75m11 5.5a1.75 1.75 0 1 1 1.75-1.75a1.76 1.76 0 0 1-1.75 1.75"/></svg>`),
	IconPlugins:   icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M9 3v4M15 3v4"/><rect x="6" y="7" width="12" height="9" rx="2"/><path d="M12 16v4"/></g></svg>`),
	IconAIGC:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" fill-rule="evenodd" d="m18.892 5.297l1.254.418a.3.3 0 0 1 0 .57l-1.254.418a.3.3 0 0 0-.19.19l-.417 1.253a.3.3 0 0 1-.57 0l-.418-1.254a.3.3 0 0 0-.19-.19l-1.253-.417a.3.3 0 0 1 0-.57l1.254-.418a.3.3 0 0 0 .19-.19l.417-1.253a.3.3 0 0 1 .57 0l.418 1.254c.03.09.1.16.19.19m-9.698-.25c.258-.775 1.354-.774 1.612 0l1.537 4.61l4.61 1.537c.774.258.774 1.354 0 1.612l-4.61 1.537l-1.537 4.61c-.258.774-1.354.774-1.612 0l-1.537-4.61l-4.61-1.537c-.775-.258-.775-1.354 0-1.612l4.61-1.537zM10 7.372L8.977 10.44a.85.85 0 0 1-.537.537L5.372 12l3.068 1.023a.85.85 0 0 1 .537.537L10 16.628l1.023-3.068a.85.85 0 0 1 .537-.537L14.628 12l-3.068-1.023a.85.85 0 0 1-.537-.537zm10.146 10.343l-1.254-.418a.3.3 0 0 1-.19-.19l-.417-1.253a.3.3 0 0 0-.57 0l-.418 1.254a.3.3 0 0 1-.19.19l-1.253.417a.3.3 0 0 0 0 .57l1.254.418c.09.03.16.1.19.19l.417 1.253a.3.3 0 0 0 .57 0l.418-1.254a.3.3 0 0 1 .19-.19l1.253-.417a.3.3 0 0 0 0-.57" clip-rule="evenodd"/></svg>`),
	IconPlus:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="2"><path d="M12 5v14M5 12h14"/></g></svg>`),
	IconTrash:     icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M10 11v6M14 11v6"/></g></svg>`),
	IconClose:     icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></g></svg>`),
	IconSearch:    icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="2"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/></g></svg>`),
	IconEdit:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="M4 20h4L18.5 9.5l-4-4L4 16zm13.7-11.3l-2.4-2.4l1.3-1.3q.3-.3.7-.3t.7.3l1 1q.3.3.3.7t-.3.7z"/></svg>`),
	IconSend:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/></g></svg>`),
	IconPanel:     icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linejoin="round" stroke-width="2"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/></g></svg>`),
	IconStop:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"/><rect x="9" y="9" width="6" height="6" rx="1" fill="currentColor"/></svg>`),
	IconMessage:   icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="M7 3h10a4 4 0 0 1 4 4v6a4 4 0 0 1-4 4H8.6l-3.8 3.4A1 1 0 0 1 3 19.6V7a4 4 0 0 1 4-4m0 2a2 2 0 0 0-2 2v9.1l2.3-2.1H17a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2z"/></svg>`),
	IconMore:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="M12 5a2 2 0 1 1 0 4a2 2 0 0 1 0-4m0 5a2 2 0 1 1 0 4a2 2 0 0 1 0-4m0 5a2 2 0 1 1 0 4a2 2 0 0 1 0-4"/></svg>`),
	IconChevron:   icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m9 6 6 6l-6 6"/></svg>`),
	IconSparkles:  icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/><path d="M20 3v4"/><path d="M22 5h-4"/></g></svg>`),
	IconImage:     icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/></g></svg>`),
	IconUser:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></g></svg>`),
	IconFileText:  icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/></g></svg>`),
	IconDashboard: icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><rect width="7" height="9" x="3" y="3" rx="1"/><rect width="7" height="5" x="14" y="3" rx="1"/><rect width="7" height="9" x="14" y="12" rx="1"/><rect width="7" height="5" x="3" y="16" rx="1"/></g></svg>`),
	IconPerson:    icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><circle cx="12" cy="5" r="1"/><path d="m9 20 3-6 3 6"/><path d="m6 8 6 2 4-2"/><path d="M12 10v4"/></g></svg>`),
	IconTasks:     icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="m3 17 2 2 4-4"/><path d="m3 7 2 2 4-4"/><path d="M13 6h8"/><path d="M13 12h8"/><path d="M13 18h8"/></g></svg>`),
	IconSun:       icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></g></svg>`),
	IconMoon:      icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></g></svg>`),
	IconMonitor:   icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><rect width="20" height="14" x="2" y="3" rx="2"/><path d="M8 21h8"/><path d="M12 17v4"/></g></svg>`),
}

func icon(data string) *ui.SVG {
	return ui.MustParseSVG([]byte(data))
}

// Icon 绘制图标，颜色跟随父级文本颜色。
func Icon(c *ui.Context, name IconName, size float32) ui.Element {
	s, ok := icons[name]
	if !ok {
		return ui.Box(c).Width(size).Height(size)
	}
	return ui.Icon(c, s).Width(size).Height(size)
}
