package tool

// 任务管理工具的 ID，与模型函数名保持一致。
const (
	ListTasksToolID  = "builtin_list_tasks"
	CancelTaskToolID = "builtin_cancel_task"
)

var taskToolIDs = map[string]bool{
	ListTasksToolID:  true,
	CancelTaskToolID: true,
}

// IsTaskTool 判断工具是否属于任务管理能力，只开放给主助手。
func IsTaskTool(id string) bool {
	return taskToolIDs[id]
}

// ListTasksInput 是查询任务的参数。
type ListTasksInput struct {
	AgentID string `json:"agentId"`
	Status  string `json:"status"`
	Query   string `json:"query"`
	Limit   int    `json:"limit"`
}

// TaskRef 是任务查询结果的一行。
type TaskRef struct {
	ID            string
	Title         string
	Status        string
	AssigneeName  string
	Attempt       int
	Error         string
	ResultSummary string
}

// TaskToolSpecs 声明查询与取消任务两个内置工具。
func TaskToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			ID:          ListTasksToolID,
			Name:        ListTasksToolID,
			Description: "查询后台任务及其状态。用户问「进度怎么样了」「刚才的任务完成了吗」时调用，可按员工或状态过滤。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "agentId": {"type": "string", "description": "可选，只查看某个员工的 ID"},
    "status": {"type": "string", "description": "可选，queued / running / done / failed / canceled / timeout"},
    "query": {"type": "string", "description": "可选，按任务标题或描述搜索历史任务"},
    "limit": {"type": "integer", "description": "最多返回条数，默认 10"}
  }
}`,
		},
		{
			ID:          CancelTaskToolID,
			Name:        CancelTaskToolID,
			Description: "取消一个排队中或运行中的后台任务。用户明确要求停止某个任务时调用。",
			Source:      SourceBuiltin,
			SourceID:    "builtin",
			Parameters: `{
  "type": "object",
  "properties": {
    "taskId": {"type": "string", "description": "任务 ID，来自 builtin_list_tasks 或派发时的返回"}
  },
  "required": ["taskId"]
}`,
		},
	}
}
