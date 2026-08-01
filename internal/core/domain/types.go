// Package domain contains the pure business logic for Aura Power.
// It has zero external dependencies and is fully deterministic.
package domain

import "time"

// PowerState represents whether a workload should be on or off.
type PowerState string

const (
	PowerStateOn  PowerState = "on"
	PowerStateOff PowerState = "off"
)

// Opposite returns the inverse power state.
func (s PowerState) Opposite() PowerState {
	switch s {
	case PowerStateOn:
		return PowerStateOff
	case PowerStateOff:
		return PowerStateOn
	default:
		return PowerStateOn // safety-first
	}
}

// IsValid returns true if the power state is a known value.
func (s PowerState) IsValid() bool {
	return s == PowerStateOn || s == PowerStateOff
}

// Priority defines the precedence of a rule. Higher values win.
type Priority int32

// Weekday represents a day of the week (0=Sunday through 6=Saturday).
type Weekday int

const (
	Sunday    Weekday = 0
	Monday    Weekday = 1
	Tuesday   Weekday = 2
	Wednesday Weekday = 3
	Thursday  Weekday = 4
	Friday    Weekday = 5
	Saturday  Weekday = 6
)

// TimeOfDay represents a time within a day (hour:minute).
type TimeOfDay struct {
	Hour   int
	Minute int
}

// ToMinutes converts the time of day to total minutes since midnight.
func (t TimeOfDay) ToMinutes() int {
	return t.Hour*60 + t.Minute
}

// IsValid returns true if the time of day has valid hour and minute values.
func (t TimeOfDay) IsValid() bool {
	return t.Hour >= 0 && t.Hour <= 23 && t.Minute >= 0 && t.Minute <= 59
}

// TimeWindow represents a recurring time segment within a day.
// If Start > End (in minutes), the window wraps past midnight.
type TimeWindow struct {
	Start    TimeOfDay
	End      TimeOfDay
	Days     []Weekday
	Timezone string // IANA timezone identifier
}

// CrossesMidnight returns true if this window wraps past midnight.
func (w TimeWindow) CrossesMidnight() bool {
	return w.Start.ToMinutes() > w.End.ToMinutes()
}

// Schedule defines when a desired state is active.
type Schedule struct {
	Windows      []TimeWindow
	DesiredState PowerState // State during windows (outside = opposite)
}

// Scope defines which workloads a policy/override targets.
// All non-empty selectors are intersected (AND logic).
type Scope struct {
	Namespaces      []string
	NamespaceLabels map[string]string
	WorkloadNames   []string
	WorkloadLabels  map[string]string
}

// ScopeSpecificity indicates how specific a scope is for tie-breaking.
type ScopeSpecificity int

const (
	ScopeClusterWide ScopeSpecificity = 0
	ScopeNamespace   ScopeSpecificity = 1
	ScopeWorkload    ScopeSpecificity = 2
)

// WorkloadKind identifies the type of Kubernetes workload.
type WorkloadKind string

const (
	WorkloadKindDeployment  WorkloadKind = "Deployment"
	WorkloadKindStatefulSet WorkloadKind = "StatefulSet"
	WorkloadKindCronJob     WorkloadKind = "CronJob"
)

// WorkloadRef uniquely identifies a workload in a cluster.
type WorkloadRef struct {
	Namespace string
	Name      string
	Kind      WorkloadKind
}

// ResourceSummary captures CPU and memory resource requests.
type ResourceSummary struct {
	CPUMillicores int64 // Total CPU request in millicores (per-pod * replicas)
	MemoryMiB     int64 // Total memory request in MiB (per-pod * replicas)
}

// Snapshot captures the state needed to restore a workload.
type Snapshot struct {
	ReplicaCount *int32
	Suspended    *bool
	Resources    ResourceSummary
	CapturedAt   time.Time
}

// ObservedState represents the current actual state of a workload.
type ObservedState struct {
	Replicas  int32
	Suspended bool
}

// PowerStateFromObserved derives the power state from observed state.
func PowerStateFromObserved(obs ObservedState, kind WorkloadKind) PowerState {
	switch kind {
	case WorkloadKindCronJob:
		if obs.Suspended {
			return PowerStateOff
		}
		return PowerStateOn
	default:
		if obs.Replicas == 0 {
			return PowerStateOff
		}
		return PowerStateOn
	}
}

// OwnershipType identifies the external management system.
type OwnershipType string

const (
	OwnershipArgoCD OwnershipType = "ArgoCD"
	OwnershipFlux   OwnershipType = "Flux"
	OwnershipHelm   OwnershipType = "Helm"
	OwnershipHPA    OwnershipType = "HPA"
)

// OwnershipSignal indicates external management of a workload.
type OwnershipSignal struct {
	Type    OwnershipType
	OptedIn bool
}

// Target represents a discovered workload under Aura Power management.
type Target struct {
	Ref             WorkloadRef
	ObservedState   ObservedState
	Ownership       []OwnershipSignal
	Annotations     map[string]string
	Labels          map[string]string
	NamespaceLabels map[string]string
	Snapshot        *Snapshot
	LastTransition  *time.Time
	DivergenceSince *time.Time
}

// PolicySpec is the domain representation of a PowerPolicy.
type PolicySpec struct {
	Name        string
	Namespace   string
	Scope       Scope
	Schedule    Schedule
	Priority    Priority
	Description string
	CreatedAt   time.Time
}

// OverrideSpec is the domain representation of a PowerOverride.
type OverrideSpec struct {
	Name      string
	Namespace string
	Scope     Scope
	State     PowerState
	Priority  Priority
	ExpiresAt time.Time
	Reason    string
	Reference string
	CreatedAt time.Time
}

// IsExpired returns true if the override has passed its expiration time.
func (o OverrideSpec) IsExpired(now time.Time) bool {
	return now.After(o.ExpiresAt)
}

// RuleKind identifies whether a rule is a policy or override.
type RuleKind string

const (
	RuleKindPolicy   RuleKind = "PowerPolicy"
	RuleKindOverride RuleKind = "PowerOverride"
)

// RuleRef identifies a rule that participated in a decision.
type RuleRef struct {
	Kind        RuleKind
	Name        string
	Namespace   string
	Priority    Priority
	Specificity ScopeSpecificity
	Description string
	CreatedAt   time.Time
}

// BlockType categorizes the reason a workload is blocked.
type BlockType string

const (
	BlockSystemNamespace  BlockType = "SystemNamespace"
	BlockArgoCDManaged   BlockType = "ArgoCDManaged"
	BlockFluxManaged     BlockType = "FluxManaged"
	BlockHelmManaged     BlockType = "HelmManaged"
	BlockHPAControlled   BlockType = "HPAControlled"
	BlockSnapshotMissing BlockType = "SnapshotMissing"
	BlockInsufficientInfo BlockType = "InsufficientInfo"
)

// BlockReason explains why a target cannot be powered down.
type BlockReason struct {
	Type     BlockType
	Message  string
	Waivable bool // Can be unblocked via opt-in annotation
}

// Decision is the computed effective state for a single target.
type Decision struct {
	DesiredState     PowerState
	WinningRule      *RuleRef
	SuppressedRules  []RuleRef
	BlockReasons     []BlockReason
	Divergent        bool
	SnapshotRequired bool
}

// IsBlocked returns true if the target has any blocking reasons.
func (d Decision) IsBlocked() bool {
	return len(d.BlockReasons) > 0
}

// IsManaged returns true if there is a winning rule (target is under governance).
func (d Decision) IsManaged() bool {
	return d.WinningRule != nil
}

// EvaluatedRule represents a rule after time-window evaluation.
type EvaluatedRule struct {
	Ref            RuleRef
	EffectiveState PowerState
	Specificity    ScopeSpecificity
}

// GuardrailConfig holds configuration for guardrail evaluation.
type GuardrailConfig struct {
	SystemNamespaces []string
	CustomBlocklist  []string
	OptInAnnotation  string
	ExemptAnnotation string
}

// DefaultGuardrailConfig returns the default guardrail configuration.
func DefaultGuardrailConfig() GuardrailConfig {
	return GuardrailConfig{
		SystemNamespaces: []string{"kube-system", "kube-public", "kube-node-lease", "aura-system"},
		CustomBlocklist:  nil,
		OptInAnnotation:  "aura.sh/power-eligible",
		ExemptAnnotation: "aura.sh/power-exempt",
	}
}

// CostConfig holds pricing for savings estimation.
type CostConfig struct {
	CPUPerHour   float64 // $/CPU-hour (default: 0.04)
	MemoryPerGiB float64 // $/GiB-hour (default: 0.008)
}

// DefaultCostConfig returns the default cost configuration.
func DefaultCostConfig() CostConfig {
	return CostConfig{
		CPUPerHour:   0.04,
		MemoryPerGiB: 0.008,
	}
}

// SavingsEstimate holds computed savings for a single target.
type SavingsEstimate struct {
	Target         WorkloadRef
	CPUHoursSaved  float64
	MemoryGiBHours float64
	EstimatedCost  float64
	OffDuration    time.Duration
	DivergenceTime time.Duration
}

// SavingsSummary aggregates savings across multiple targets.
type SavingsSummary struct {
	TotalCPUHours  float64
	TotalMemoryGiB float64
	TotalCost      float64
	ByNamespace    map[string]SavingsEstimate
	ByPolicy       map[string]SavingsEstimate
}

// PreviewResult holds the impact preview of a proposed policy/override.
type PreviewResult struct {
	AffectedOn    []WorkloadRef
	AffectedOff   []WorkloadRef
	Blocked       []BlockedTarget
	Unsupported   []WorkloadRef
	Conflicts     []ConflictInfo
	TotalAffected int
}

// BlockedTarget represents a target blocked with its reasons.
type BlockedTarget struct {
	Ref     WorkloadRef
	Reasons []BlockReason
}

// ConflictInfo shows how competing rules were resolved for a target.
type ConflictInfo struct {
	Target     WorkloadRef
	Winner     RuleRef
	Suppressed []RuleRef
}
