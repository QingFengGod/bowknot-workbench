package contract

// TaskStatus 是后台任务的状态，与 domain 层保持一致。
type TaskStatus string

const (
	TaskQueued   TaskStatus = "queued"
	TaskRunning  TaskStatus = "running"
	TaskDone     TaskStatus = "done"
	TaskFailed   TaskStatus = "failed"
	TaskCanceled TaskStatus = "canceled"
	TaskTimeout  TaskStatus = "timeout"
)

// Terminal 判断任务是否已进入终态。
func (s TaskStatus) Terminal() bool {
	switch s {
	case TaskDone, TaskFailed, TaskCanceled, TaskTimeout:
		return true
	}
	return false
}

// TaskBrief 是消息列表里关联任务的摘要，供 UI 直接渲染任务卡片。
type TaskBrief struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Status       TaskStatus `json:"status"`
	AssigneeID   string     `json:"assigneeId"`
	AssigneeName string     `json:"assigneeName"`
}

// TaskDetail 是任务列表与任务卡片使用的完整任务信息。
type TaskDetail struct {
	ID                   string     `json:"id"`
	Title                string     `json:"title"`
	Brief                string     `json:"brief"`
	Status               TaskStatus `json:"status"`
	AssigneeID           string     `json:"assigneeId"`
	AssigneeName         string     `json:"assigneeName"`
	OriginConversationID string     `json:"originConversationId"`
	OriginTitle          string     `json:"originTitle"`
	ThreadConversationID string     `json:"threadConversationId"`
	Attempt              int        `json:"attempt"`
	ResultSummary        string     `json:"resultSummary"`
	Error                string     `json:"error"`
	CreatedAt            int64      `json:"createdAt"`
	StartedAt            int64      `json:"startedAt"`
	FinishedAt           int64      `json:"finishedAt"`
	// DurationMs 是已结束任务的执行耗时；未开始或未结束时为 0。
	DurationMs int64 `json:"durationMs"`
}

// TaskStats 是一段时间内的任务统计。
type TaskStats struct {
	Total         int     `json:"total"`
	Queued        int     `json:"queued"`
	Running       int     `json:"running"`
	Done          int     `json:"done"`
	Failed        int     `json:"failed"`
	Canceled      int     `json:"canceled"`
	Timeout       int     `json:"timeout"`
	SuccessRate   float64 `json:"successRate"`
	AvgDurationMs int64   `json:"avgDurationMs"`
}

// AgentTaskStats 是按员工聚合的任务统计。
type AgentTaskStats struct {
	AgentID   string    `json:"agentId"`
	AgentName string    `json:"agentName"`
	Stats     TaskStats `json:"stats"`
}

// TaskAudit 是任务审计页需要的数据。
type TaskAudit struct {
	Overall TaskStats        `json:"overall"`
	Agents  []AgentTaskStats `json:"agents"`
	Recent  []TaskDetail     `json:"recent"`
}

// TaskService 是任务面板与任务卡片依赖的能力接口。
type TaskService interface {
	// Tasks 按员工与状态筛选任务，agentID / status 为空表示不过滤。
	Tasks(agentID string, status TaskStatus, limit int) []TaskDetail
	Task(id string) TaskDetail
	// TaskAudit 返回最近 days 天的统计与最近任务；days <= 0 表示全部时间。
	TaskAudit(days int) TaskAudit
	// SearchTasks 按标题与任务描述检索历史任务，供跨任务查找。
	SearchTasks(query string, limit int) []TaskDetail
	// DispatchTask 供 UI 的显式「转交」入口创建任务，来源会话用于写回汇报。
	DispatchTask(agentID, brief, originConversationID string) (TaskDetail, error)
	// TaskMessages 返回任务线程的消息，供只读的过程抽屉使用。
	TaskMessages(id string) []ChatMessage
	CancelTask(id string) error
	RetryTask(id string) error
	// TaskRevision 在任务状态变化时递增，UI 据此刷新卡片。
	TaskRevision() uint64
}
