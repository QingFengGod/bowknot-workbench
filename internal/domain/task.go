package domain

// TaskStatus 是后台任务的状态。
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

// AssignerKind 标识任务派发方类型；P1 只使用主助手。
type AssignerKind string

const (
	AssignerMain AssignerKind = "main"
)

// Task 是主助手派给某个 Agent 的一次可追踪任务。
type Task struct {
	ID                   string
	Title                string
	Brief                string
	Status               TaskStatus
	AssignerKind         AssignerKind
	AssignerID           string
	AssigneeID           string
	OriginConversationID string
	OriginMessageID      string
	ThreadConversationID string
	ParentTaskID         string
	GroupID              string
	Attempt              int
	ResultSummary        string
	ResultMessageID      string
	Error                string
	CreatedAt            int64
	StartedAt            int64
	FinishedAt           int64
	ReportedAt           int64
}
