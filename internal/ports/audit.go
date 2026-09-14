package ports

import (
	"context"
	"time"

	"github.com/weauratech/aura-power/internal/core/domain"
)

// AuditAction identifies the type of audit event.
type AuditAction string

const (
	AuditPolicyCreated       AuditAction = "policy.created"
	AuditPolicyModified      AuditAction = "policy.modified"
	AuditPolicyDeleted       AuditAction = "policy.deleted"
	AuditOverrideCreated     AuditAction = "override.created"
	AuditOverrideExpired     AuditAction = "override.expired"
	AuditWorkloadPoweredDown AuditAction = "workload.powered_down"
	AuditWorkloadRestored    AuditAction = "workload.restored"
	AuditActionBlocked       AuditAction = "action.blocked"
	AuditExecutionError      AuditAction = "execution.error"
	AuditDivergenceDetected  AuditAction = "divergence.detected"
	AuditWorkloadOptedIn     AuditAction = "workload.opted_in"
)

const (
	// NotificationPolicyLabel lets a namespace opt out of external delivery
	// while retaining the complete durable audit trail.
	NotificationPolicyLabel    = "power.aura.sh/notification-policy"
	NotificationPolicyDisabled = "disabled"
)

// AuditEvent represents a structured audit record.
type AuditEvent struct {
	// ID requests an idempotent, deterministic record name. Empty IDs retain
	// append-only generated-name behavior for callers without a durable saga.
	ID        string
	Timestamp time.Time
	Action    AuditAction
	Actor     string // "system/policy", "system/override", "user/<name>"
	Target    domain.WorkloadRef
	Result    string // "success", "blocked", "error"
	Reason    string
	RuleName  string // Name of the policy/override responsible
	// SuppressNotification keeps the durable audit record but prevents this
	// event from being placed on any external notification channel.
	SuppressNotification                bool
	NotificationSuppressionSource       string
	NotificationSuppressionNamespaceUID string
}

// AuditListOptions provides filtering for audit events.
type AuditListOptions struct {
	Target *domain.WorkloadRef
	Action *AuditAction
	Since  *time.Time
	Limit  int
}

// AuditRecorder creates and queries audit events.
type AuditRecorder interface {
	Record(ctx context.Context, event AuditEvent) error
	List(ctx context.Context, opts AuditListOptions) ([]AuditEvent, error)
}
