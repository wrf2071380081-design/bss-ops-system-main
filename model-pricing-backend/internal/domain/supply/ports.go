package supply

import (
	"context"
	"time"
)

// WriteMeta must be constructed from authenticated application context, not HTTP body actor fields.
type WriteMeta struct {
	ActorID   string
	Reason    string
	RequestID string
	// CanMaintain 表示调用方除维护人之外另有的维护授权（当前由 API 层按 M1:E 判定）。
	// 领域层不认识权限字符串：策略在上层决定，这里只承载结论。
	// 维护人本人始终可维护，与 CanMaintain 无关；核验动作不看这个字段。
	CanMaintain bool
}
type ModelOperation struct {
	EntityType string
	EntityID   string
	Action     string
	ActorID    string
	Reason     string
	RequestID  string
	Before     *StandardModel
	After      *StandardModel
	OccurredAt time.Time
}

// ModelUnitOfWork joins an existing caller transaction when provided. All effects in run,
// including version and operation writes, must roll back together on any error or commit failure.
// This is a port, not a second transaction manager implementation.
type ModelUnitOfWork interface {
	Within(context.Context, func(ModelTransaction) error) error
}

type ModelTransaction interface {
	NextID(context.Context) (string, error)
	GetForUpdate(context.Context, string) (*StandardModel, error)
	Create(context.Context, *StandardModel) error
	// Update must enforce CAS as well as official-identity uniqueness at the database boundary.
	Update(context.Context, *StandardModel, uint64) error
	AppendVersion(context.Context, *StandardModel) error
	AppendOperation(context.Context, ModelOperation) error
}

// OfficialSourcePolicy validates against a configured official-source policy and known facts.
// It MUST NOT fetch pages, invoke an LLM or perform other network I/O within the SQL transaction.
// Network observation belongs in an earlier collection stage, with versioned evidence.
type OfficialSourcePolicy interface {
	Validate(context.Context, string, []Source) error
}
