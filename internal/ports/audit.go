package ports

import (
	"context"
	"time"

	"github.com/weauratech/aura-power/internal/core/domain"
)

// AuditAction identifies the type of audit event.
type AuditAction string

const (
	AuditPolicyCreated     AuditAction = "policy.created"
	AuditPolicyModified    AuditAction = "policy.modified"
	AuditPolicyDeleted     AuditAction = "policy.deleted"
	AuditOverrideCreated   AuditAction = "override.created"
	AuditOverrideExpired   AuditAction = "override.expired"
	AuditWorkloadPoweredDown AuditAction = "workload.powered_down"
	AuditWorkloadRestored  AuditAction = "workload.restored"
	AuditActionBlocked     AuditAction = "action.blocked"
	AuditExecutionError    AuditAction = "execution.error"
	AuditDivergenceDetected AuditAction = "divergence.detected"
	AuditWorkloadOptedIn   AuditAction = "workload.opted_in"
)

// AuditEvent represents a structured audit record.
type AuditEvent struct {
	Timestamp time.Time
	Action    AuditAction
	Actor     string // "system/policy", "system/override", "user/<name>"
	Target    domain.WorkloadRef
	Result    string // "success", "blocked", "error"
	Reason    string
	RuleName  string // Name of the policy/override responsible
}

// AuditListOptions provides filtering for audit events.
type AuditListOptions struct {
	Target    *domain.WorkloadRef
	Action    *AuditAction
	Since     *time.Time
	Limit     int
}

// AuditRecorder creates and queries audit events.
type AuditRecorder interface {
	Record(ctx context.Context, event AuditEvent) error
	List(ctx context.Context, opts AuditListOptions) ([]AuditEvent, error)
}
