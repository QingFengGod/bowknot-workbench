package tool

// 记忆工具 ID；与内置工具保持一致，避免模型供应商对函数名的字符限制。
const (
	RememberToolID     = "builtin_remember"
	SearchMemoryToolID = "builtin_search_memory"
	UpdateMemoryToolID = "builtin_update_memory"
	ForgetMemoryToolID = "builtin_forget_memory"
)

var memoryToolIDs = map[string]bool{
	RememberToolID:     true,
	SearchMemoryToolID: true,
	UpdateMemoryToolID: true,
	ForgetMemoryToolID: true,
}

// IsMemoryTool 判断工具是否属于长期记忆工具。
// UI 与事件层据此隐藏工具卡片，记忆操作只在消息上留一个标记。
func IsMemoryTool(id string) bool {
	return memoryToolIDs[id]
}

// RememberInput 是写入长期记忆的参数。
// 记忆归属由宿主按调用方 Agent 推导，模型不能指定 scope 或 agentId。
type RememberInput struct {
	Content string `json:"content"`
	// Kind 取值 profile / preference / fact / decision / event，缺省按 preference 处理。
	Kind string `json:"kind"`
	// OccurredAt 是事情发生的时间，缺省为当前时间。
	OccurredAt string `json:"occurred_at"`
}

// SearchMemoryInput 是检索长期记忆的参数。
type SearchMemoryInput struct {
	Query string `json:"query"`
	// From / To 是可选的自然日范围，格式 YYYY-MM-DD。
	From string `json:"from"`
	To   string `json:"to"`
	// Limit 是返回条数，缺省 8 条，最多 20 条。
	Limit int `json:"limit"`
}

// UpdateMemoryInput 是修改长期记忆的参数；ID 来自检索结果。
type UpdateMemoryInput struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// ForgetMemoryInput 是删除长期记忆的参数；ID 来自检索结果。
type ForgetMemoryInput struct {
	ID string `json:"id"`
}

// MemoryToolSpecs 返回四个内置记忆工具的声明。
func MemoryToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			ID:   RememberToolID,
			Name: RememberToolID,
			Description: "把用户透露的长期信息写入记忆。触发条件：用户这轮说到了自己的身份或背景（姓名、职业、所在城市、项目）、偏好与习惯、长期目标或计划、重要决定、值得记住的经历，或对你提出长期有效的称呼、语气、格式要求。" +
				"命中任意一类时必须先调用本工具再回复，不要等用户要求，也不要向用户宣告。" +
				"一次只写一条，用第三人称完整陈述：指代用户一律用「用户」，指代你自己用「我」，不要用「你」指代用户；不超过 200 字，一轮最多写两条。" +
				"不要写入普通问答、一次性任务、临时情绪、寒暄、转述他人的信息，以及密码、密钥等敏感内容。" +
				"示例：用户说「我以后都用简体中文回复」→ 写入「用户希望以后都用简体中文回复」；用户问「今天天气怎么样」→ 不写入。",
			Source:   SourceBuiltin,
			SourceID: "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "content": {"type": "string", "description": "一条完整、可独立理解的记忆，用第三人称：用户用「用户」，你自己用「我」，例如「用户喜欢喝美式咖啡」"},
    "kind": {"type": "string", "enum": ["profile", "preference", "fact", "decision", "event"], "description": "记忆类型，缺省为 preference"},
    "occurred_at": {"type": "string", "description": "事情发生的时间，RFC3339 或 YYYY-MM-DD；不确定时省略"}
  },
  "required": ["content"]
}`,
		},
		{
			ID:          SearchMemoryToolID,
			Name:        SearchMemoryToolID,
			Description: "检索长期记忆。用户提到以前的事、要求回忆某个细节，或你需要过往信息才能回答时，先调用本工具，不要凭空猜测。结果会返回记忆 ID，修改或删除前必须先检索拿到 ID。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "要查找的关键词或主题，例如「支付重构」「去年的旅行」"},
    "from": {"type": "string", "description": "起始日期，格式 YYYY-MM-DD，可选"},
    "to": {"type": "string", "description": "结束日期，格式 YYYY-MM-DD，可选"},
    "limit": {"type": "integer", "description": "返回条数，缺省 8，最多 20"}
  },
  "required": ["query"]
}`,
		},
		{
			ID:          UpdateMemoryToolID,
			Name:        UpdateMemoryToolID,
			Description: "修改一条已有的长期记忆。用户纠正旧信息或要求改口时调用；必须先通过检索工具拿到记忆 ID，不要编造 ID。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "记忆 ID，来自检索结果"},
    "content": {"type": "string", "description": "修改后的完整内容，不超过 200 字"}
  },
  "required": ["id", "content"]
}`,
		},
		{
			ID:          ForgetMemoryToolID,
			Name:        ForgetMemoryToolID,
			Description: "删除一条长期记忆。用户明确要求忘记某事时调用；必须先通过检索工具拿到记忆 ID，不要编造 ID。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "记忆 ID，来自检索结果"}
  },
  "required": ["id"]
}`,
		},
	}
}
