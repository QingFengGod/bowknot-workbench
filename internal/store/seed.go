package store

import (
	"context"
	"errors"
	"time"

	"bowknot-workbench/internal/domain"
)

const (
	seedMetaKey = "seed_version"
	seedVersion = "demo.v1"
)

// seedDemo 只在开发版首次启动时导入一次演示数据。
func (s *Store) seedDemo(ctx context.Context) error {
	return s.Tx(ctx, func(tx *Store) error {
		version, err := tx.Meta.Get(ctx, seedMetaKey)
		if err == nil && version != "" {
			return nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}

		if err := seedAssistant(ctx, tx); err != nil {
			return err
		}
		if err := seedAgents(ctx, tx); err != nil {
			return err
		}
		if err := seedApps(ctx, tx); err != nil {
			return err
		}
		if err := seedPlugins(ctx, tx); err != nil {
			return err
		}
		return tx.Meta.Set(ctx, seedMetaKey, seedVersion)
	})
}

func seedAssistant(ctx context.Context, tx *Store) error {
	now := time.Now().UnixMilli()
	const minute = int64(60 * 1000)
	const hour = 60 * minute
	const day = 24 * hour

	conversations := []domain.Conversation{
		{ID: "daily", Kind: domain.KindAssistant, OwnerID: "main", Title: "日常", Summary: "下周计划已经整理好，按天推进即可。", CreatedAt: now - day, UpdatedAt: now - 2*minute},
		{ID: "work", Kind: domain.KindAssistant, OwnerID: "main", Title: "工作讨论", Summary: "周报要点分三块：本周进展、风险与下周计划。", CreatedAt: now - 2*day, UpdatedAt: now - 36*minute},
		{ID: "travel", Kind: domain.KindAssistant, OwnerID: "main", Title: "旅行计划", Summary: "杭州三天两晚，按西湖、灵隐、龙井的顺序排。", CreatedAt: now - 3*day, UpdatedAt: now - 5*hour},
		{ID: "reading", Kind: domain.KindAssistant, OwnerID: "main", Title: "读书笔记", Summary: "把读书笔记的摘录按章节归档。", CreatedAt: now - 4*day, UpdatedAt: now - day - 3*hour},
		{ID: "fitness", Kind: domain.KindAssistant, OwnerID: "main", Title: "健身打卡", Summary: "本周完成 3 次力量训练，下一次建议练背。", CreatedAt: now - 5*day, UpdatedAt: now - 3*day},
	}
	for _, item := range conversations {
		if err := tx.Conversations.Create(ctx, item); err != nil {
			return err
		}
	}

	messages := []domain.Message{
		{ID: "daily-u1", ConversationID: "daily", Role: domain.RoleUser, Content: "帮我整理一份下周的工作计划，越详细越好。", Status: domain.MessageDone, CreatedAt: now - 3*minute},
		{ID: "work-u1", ConversationID: "work", Role: domain.RoleUser, Content: "帮我把这周的进展整理成周报要点。", Status: domain.MessageDone, CreatedAt: now - 40*minute},
		{ID: "work-a1", ConversationID: "work", Role: domain.RoleAssistant, Content: "本周进展可以拆成三块：消息渲染、交互闭环、待观察项。", Status: domain.MessageDone, CreatedAt: now - 39*minute, MemoryWritten: true, Emotion: "happy"},
		{ID: "travel-u1", ConversationID: "travel", Role: domain.RoleUser, Content: "杭州三天怎么玩？帮我排个不赶的行程。", Status: domain.MessageDone, CreatedAt: now - 5*hour - 20*minute},
		{ID: "travel-a1", ConversationID: "travel", Role: domain.RoleAssistant, Content: "杭州三天两晚可以按西湖、灵隐、龙井的顺序安排。", Status: domain.MessageStopped, CreatedAt: now - 5*hour - 19*minute},
		{ID: "reading-u1", ConversationID: "reading", Role: domain.RoleUser, Content: "把上次的读书笔记按章节重新整理一下。", Status: domain.MessageDone, CreatedAt: now - day - 4*hour},
		{ID: "reading-a1", ConversationID: "reading", Role: domain.RoleAssistant, Content: "我先按章节把摘录重新排了一遍。", Status: domain.MessageFailed, Error: "模型服务暂时不可用，请稍后重试", CreatedAt: now - day - 4*hour + minute},
		{ID: "fitness-u1", ConversationID: "fitness", Role: domain.RoleUser, Content: "本周的健身记录帮我总结一下。", Status: domain.MessageDone, CreatedAt: now - 3*day},
	}
	return appendMessages(ctx, tx, messages)
}

func seedAgents(ctx context.Context, tx *Store) error {
	now := time.Now().UnixMilli()
	const minute = int64(60 * 1000)
	const hour = 60 * minute
	const day = 24 * hour

	agents := []domain.Agent{
		{ID: "writing", Name: "写作助手", Description: "负责长文撰写与润色", AvatarText: "写", ModelID: "bk-pro", Persona: "你是一位克制、讲究节奏的中文写作者，擅长把生硬的内容改得更像人写的。", Pinned: true, CreatedAt: now - 12*day, UpdatedAt: now - day},
		{ID: "research", Name: "研究助手", Description: "搜集资料与竞品调研", AvatarText: "研", ModelID: "bk-pro", Persona: "你负责查找资料、核对事实，并给出带来源的结论。", CreatedAt: now - 11*day, UpdatedAt: now - day},
		{ID: "coding", Name: "代码搭档", Description: "读代码、改 bug、写测试", AvatarText: "码", ModelID: "bk-reason", Persona: "你是一位严谨的工程搭档，先读代码再给方案，改动保持最小。", CreatedAt: now - 10*day, UpdatedAt: now - day},
		{ID: "schedule", Name: "日程管家", Description: "管理日程与提醒", AvatarText: "程", ModelID: "bk-flash", Persona: "你负责整理日程、设置提醒，并在冲突时给出可选方案。", CreatedAt: now - 9*day, UpdatedAt: now - day},
	}
	for _, item := range agents {
		if err := tx.Agents.Upsert(ctx, item); err != nil {
			return err
		}
	}

	groups := []domain.Group{
		{ID: "weekly", Name: "周报小组", MemberIDs: []string{"writing", "research"}, Pinned: true, CreatedAt: now - 8*day},
		{ID: "product", Name: "产品讨论组", MemberIDs: []string{"research", "coding"}, CreatedAt: now - 7*day},
	}
	for _, item := range groups {
		if err := tx.Groups.Upsert(ctx, item); err != nil {
			return err
		}
	}

	conversations := []domain.Conversation{
		{ID: "writing-report", Kind: domain.KindAgent, OwnerID: "writing", Title: "周报润色", Summary: "帮我把这份周报改得更像人写的", Pinned: true, CreatedAt: now - day, UpdatedAt: now - 3*minute},
		{ID: "writing-article", Kind: domain.KindAgent, OwnerID: "writing", Title: "公众号长文", Summary: "还没有消息", CreatedAt: now - 2*day, UpdatedAt: now - 2*hour},
		{ID: "research-competitor", Kind: domain.KindAgent, OwnerID: "research", Title: "竞品调研", Summary: "整理近三个月的版本更新", CreatedAt: now - 3*day, UpdatedAt: now - 40*minute},
		{ID: "coding-scroll", Kind: domain.KindAgent, OwnerID: "coding", Title: "MessageList 滚动问题", Summary: "切换会话后没有回到底部", CreatedAt: now - 4*day, UpdatedAt: now - hour},
		{ID: "schedule-week", Kind: domain.KindAgent, OwnerID: "schedule", Title: "本周日程", Summary: "周报评审与竞品对比截止时间", CreatedAt: now - 5*day, UpdatedAt: now - 5*hour},
		{ID: "group-weekly", Kind: domain.KindGroup, OwnerID: "weekly", Title: "周报小组", Summary: "群聊", CreatedAt: now - 8*day, UpdatedAt: now - 27*minute},
	}
	for _, item := range conversations {
		if err := tx.Conversations.Create(ctx, item); err != nil {
			return err
		}
	}

	messages := []domain.Message{
		{ID: "writing-report-u1", ConversationID: "writing-report", Role: domain.RoleUser, Content: "帮我把这份周报改得更像人写的", Status: domain.MessageDone, CreatedAt: now - 4*minute},
		{ID: "writing-report-a1", ConversationID: "writing-report", Role: domain.RoleAssistant, Content: "可以，我会保留信息密度，把语气改得更像日常表达。", Status: domain.MessageDone, CreatedAt: now - 3*minute},
		{ID: "research-u1", ConversationID: "research-competitor", Role: domain.RoleUser, Content: "整理一下近三个月的竞品更新", Status: domain.MessageDone, CreatedAt: now - 41*minute},
		{ID: "research-a1", ConversationID: "research-competitor", Role: domain.RoleAssistant, Content: "已按版本、功能和价格三块整理，重点变化集中在中旬的两个版本。", Status: domain.MessageDone, CreatedAt: now - 40*minute},
		{ID: "coding-u1", ConversationID: "coding-scroll", Role: domain.RoleUser, Content: "看看 MessageList 切换会话时为什么不回到底部", Status: domain.MessageDone, CreatedAt: now - 61*minute},
		{ID: "coding-a1", ConversationID: "coding-scroll", Role: domain.RoleAssistant, Content: "问题出在切换会话后没有重置吸底标记，补一个会话级滚动状态即可。", Status: domain.MessageDone, CreatedAt: now - 60*minute},
		{ID: "schedule-u1", ConversationID: "schedule-week", Role: domain.RoleUser, Content: "这周还有什么没确认的？", Status: domain.MessageDone, CreatedAt: now - 301*minute},
		{ID: "schedule-a1", ConversationID: "schedule-week", Role: domain.RoleAssistant, Content: "还有两项：周报评审时间、竞品对比的截止时间。", Status: domain.MessageDone, CreatedAt: now - 300*minute},
		{ID: "weekly-u1", ConversationID: "group-weekly", Role: domain.RoleUser, Content: "把这周的工作整理成周报", Status: domain.MessageDone, CreatedAt: now - 30*minute},
		{ID: "weekly-a1", ConversationID: "group-weekly", Role: domain.RoleAssistant, Content: "收到，我先拉一下这周的进展和风险项。", Status: domain.MessageDone, SenderName: "研究助手", SenderAvatar: "研", CreatedAt: now - 29*minute},
		{ID: "weekly-a2", ConversationID: "group-weekly", Role: domain.RoleAssistant, Content: "结构我来出：本周进展、风险与阻塞、下周计划，三块就够。", Status: domain.MessageDone, SenderName: "写作助手", SenderAvatar: "写", CreatedAt: now - 28*minute},
		{ID: "weekly-a3", ConversationID: "group-weekly", Role: domain.RoleAssistant, Content: "汇总好了，已按这三块整理成初稿。", Status: domain.MessageDone, SenderName: "助理", SenderAvatar: "助", CreatedAt: now - 27*minute},
	}
	return appendMessages(ctx, tx, messages)
}

func seedApps(ctx context.Context, tx *Store) error {
	now := time.Now().UnixMilli()
	const minute = int64(60 * 1000)
	const hour = 60 * minute
	const day = 24 * hour

	apps := []domain.App{
		{
			ID: "password-box", Name: "密码箱", DisplayName: "密码箱", Version: "1.0.2",
			Description: "本地保存账号密码，支持助理代填", IconText: "密",
			SourceType: "local", SourceValue: `D:\bowknot-apps\password-box`,
			InstalledAt: now - 12*day, LastUsedAt: now - 2*minute, DataSize: 18 * 1024,
			Tools: []domain.Tool{
				{Name: "addPassword", Description: "新增一条账号密码记录", Enabled: true},
				{Name: "listPasswords", Description: "列出已保存的账号，不返回明文密码", Enabled: true},
				{Name: "deletePassword", Description: "删除指定账号密码记录", Dangerous: true, Enabled: true},
			},
		},
		{
			ID: "weekly-report", Name: "weekly-report", DisplayName: "周报助手", Version: "1.2.0",
			Description: "根据本周的工作记录自动生成周报", IconText: "周",
			SourceType: "remote", SourceValue: "https://apps.bowknot.dev/weekly-report",
			InstalledAt: now - 8*day, LastUsedAt: now - hour, DataSize: 1638 * 1024,
			Tools: []domain.Tool{
				{Name: "generateWeeklyReport", Description: "汇总本周工作并生成周报草稿", Enabled: true},
				{Name: "exportWeeklyReport", Description: "把周报导出为 Markdown 文件", Enabled: true},
			},
		},
		{
			ID: "quick-note", Name: "quick-note", DisplayName: "灵感速记", Version: "0.9.1",
			Description: "随手记下一句话，空闲时再整理", IconText: "灵",
			SourceType: "local", SourceValue: `D:\bowknot-apps\quick-note`,
			InstalledAt: now - 3*day, DataSize: 4300,
			Tools: []domain.Tool{
				{Name: "addNote", Description: "记录一条灵感或待办", Enabled: true},
				{Name: "searchNotes", Description: "按关键词搜索已记录的灵感", Enabled: true},
			},
		},
		{
			ID: "data-board", Name: "data-board", DisplayName: "数据看板", Version: "2.0.0",
			Description: "把本地数据整理成可读的看板", IconText: "数",
			SourceType: "remote", SourceValue: "https://apps.bowknot.dev/data-board",
			InstalledAt: now - 20*day, LastUsedAt: now - 5*day, DataSize: 8 * 1024 * 1024,
			Tools: []domain.Tool{
				{Name: "queryData", Description: "按条件查询本地数据并汇总", Enabled: true},
				{Name: "clearCache", Description: "清除看板缓存与聚合结果", Dangerous: true, Enabled: false},
			},
		},
		{
			ID: "file-organizer", Name: "file-organizer", DisplayName: "文件整理", Version: "1.4.3",
			Description: "批量重命名与归档本地文件", IconText: "文",
			SourceType: "local", SourceValue: `D:\bowknot-apps\file-organizer`,
			InstalledAt: now - 26*day, LastUsedAt: now - 3*hour, DataSize: 2 * 1024 * 1024,
		},
	}
	for _, item := range apps {
		if err := tx.Apps.Insert(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func seedPlugins(ctx context.Context, tx *Store) error {
	now := time.Now().UnixMilli()
	const day = int64(24 * 60 * 60 * 1000)

	plugins := []domain.Plugin{
		{
			ID: "web-search", Name: "网页搜索", Version: "1.2.0",
			Description: "联网检索资料并整理成可引用的摘要", IconText: "搜",
			SourceType: "remote", SourceValue: "https://plugins.bowknot.dev/web-search",
			InstalledAt: now - 9*day, AgentIDs: []string{"main-agent", "research"},
			Tools: []domain.Tool{
				{Name: "searchWeb", Description: "按关键词搜索网页并返回结果摘要"},
				{Name: "readPage", Description: "读取指定网页的正文内容"},
			},
		},
		{
			ID: "file-system", Name: "文件系统", Version: "0.8.4",
			Description: "读取、写入与整理本地文件", IconText: "文",
			SourceType: "local", SourceValue: `D:\bowknot-plugins\file-system`,
			InstalledAt: now - 4*day, AgentIDs: []string{"main-agent", "coding"},
			Tools: []domain.Tool{
				{Name: "listFiles", Description: "列出指定目录下的文件与子目录"},
				{Name: "readFile", Description: "读取文本文件内容"},
				{Name: "writeFile", Description: "写入或覆盖文本文件", Dangerous: true},
			},
		},
		{
			ID: "code-runner", Name: "代码执行", Version: "1.0.1",
			Description: "在受限环境中运行脚本与命令", IconText: "码",
			SourceType: "local", SourceValue: `D:\bowknot-plugins\code-runner`,
			InstalledAt: now - 2*day, AgentIDs: []string{"coding"},
			Tools: []domain.Tool{
				{Name: "runScript", Description: "运行一段脚本并返回输出"},
				{Name: "runCommand", Description: "执行一条本地命令", Dangerous: true},
			},
		},
		{
			ID: "image-gen", Name: "图像生成", Version: "2.1.0",
			Description: "根据描述生成或编辑图片", IconText: "图",
			SourceType: "remote", SourceValue: "https://plugins.bowknot.dev/image-gen",
			InstalledAt: now - 16*day,
			Tools: []domain.Tool{
				{Name: "generateImage", Description: "根据文字描述生成图片"},
				{Name: "editImage", Description: "按指令修改已有图片", Dangerous: true},
			},
		},
		{
			ID: "calendar", Name: "日历", Version: "1.0.0",
			Description: "查看日程、创建提醒与处理时间冲突", IconText: "历",
			SourceType: "local", SourceValue: `D:\bowknot-plugins\calendar`,
			InstalledAt: now - day, AgentIDs: []string{"main-agent", "schedule"},
			Tools: []domain.Tool{
				{Name: "listEvents", Description: "列出指定时间范围内的日程"},
				{Name: "createEvent", Description: "创建一条日程或提醒"},
				{Name: "deleteEvent", Description: "删除指定日程", Dangerous: true},
			},
		},
	}
	for _, item := range plugins {
		if err := tx.Plugins.Insert(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func appendMessages(ctx context.Context, tx *Store, messages []domain.Message) error {
	for _, item := range messages {
		if _, err := tx.Messages.Append(ctx, item); err != nil {
			return err
		}
	}
	return nil
}
