package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerTargetSpec identifies the workload being managed.
type PowerTargetSpec struct {
	// TargetRef identifies the workload.
	TargetRef TargetReference `json:"targetRef"`
}

// TargetReference uniquely identifies a workload in the cluster.
type TargetReference struct {
	// Cluster identifies the source cluster when targets are aggregated.
	// +optional
	Cluster string `json:"cluster,omitempty"`

	// APIVersion identifies the Kubernetes API group and version.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// Namespace of the workload.
	Namespace string `json:"namespace"`

	// Name of the workload.
	Name string `json:"name"`

	// Kind of the workload (Deployment, StatefulSet, CronJob).
	// +kubebuilder:validation:Enum=Deployment;StatefulSet;CronJob
	Kind string `json:"kind"`

	// UID binds the target to one concrete Kubernetes object incarnation.
	// +optional
	UID string `json:"uid,omitempty"`
}

// PowerTargetStatus defines the observed and computed state.
type PowerTargetStatus struct {
	// WorkloadLabels is the workload label snapshot used for policy selection.
	// +optional
	WorkloadLabels map[string]string `json:"workloadLabels,omitempty"`

	// NamespaceLabels is the namespace label snapshot used for policy selection.
	// +optional
	NamespaceLabels map[string]string `json:"namespaceLabels,omitempty"`

	// ObservedState is the current actual state of the workload.
	ObservedState ObservedStateSpec `json:"observedState,omitempty"`

	// DesiredState is the computed effective desired state.
	// +kubebuilder:validation:Enum=on;off;""
	DesiredState string `json:"desiredState,omitempty"`

	// Managed indicates if there is at least one governing rule.
	Managed bool `json:"managed,omitempty"`

	// Divergent indicates the observed state differs from desired.
	Divergent bool `json:"divergent,omitempty"`

	// WinningRule is the rule that determined the desired state.
	// +optional
	WinningRule *RuleReference `json:"winningRule,omitempty"`

	// SuppressedRules are rules that lost in priority resolution.
	// +optional
	SuppressedRules []RuleReference `json:"suppressedRules,omitempty"`

	// Blocked indicates the target has guardrail blocks.
	Blocked bool `json:"blocked,omitempty"`

	// BlockReasons lists all active block reasons.
	// +optional
	BlockReasons []BlockReasonSpec `json:"blockReasons,omitempty"`

	// Snapshot holds the captured state for restoration.
	// +optional
	Snapshot *SnapshotSpec `json:"snapshot,omitempty"`

	// Ownership lists detected external management signals.
	// +optional
	Ownership []OwnershipSpec `json:"ownership,omitempty"`

	// Savings holds accumulated savings metrics.
	// +optional
	Savings *SavingsSpec `json:"savings,omitempty"`

	// LastTransition is the last time the effective state changed.
	// +optional
	LastTransition *metav1.Time `json:"lastTransition,omitempty"`

	// LastReconciliation is the last time this target was reconciled.
	// +optional
	LastReconciliation *metav1.Time `json:"lastReconciliation,omitempty"`

	// ConsecutiveFailures tracks how many reconcile cycles have failed in a row.
	// +optional
	ConsecutiveFailures int `json:"consecutiveFailures,omitempty"`

	// Action records the durable state of the current power transition. The
	// controller persists InProgress before touching the workload so a repeated
	// reconcile never blindly reapplies a successful or uncertain mutation.
	// +optional
	Action *PowerActionStatus `json:"action,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PowerActionStatus records one desired-state transition.
type PowerActionStatus struct {
	// DesiredState is the state this operation is trying to establish.
	// +kubebuilder:validation:Enum=on;off
	DesiredState string `json:"desiredState"`

	// DecisionKey identifies the winning rule revision that requested the state.
	DecisionKey string `json:"decisionKey"`

	// Phase is the latest durable operation phase.
	// +kubebuilder:validation:Enum=InProgress;Applied;Converged;Contended;Failed
	Phase string `json:"phase"`

	// AttemptedAt is set before mutating the workload. It is absent when the
	// desired state converged without an Aura write.
	// +optional
	AttemptedAt *metav1.Time `json:"attemptedAt,omitempty"`

	// CompletedAt is set after a successful mutation or convergence observation.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// Message explains failures and external-controller contention.
	// +optional
	Message string `json:"message,omitempty"`

	// RetryToken is the value of power.aura.sh/retry-action accepted for this attempt.
	// Changing that annotation explicitly authorizes one new attempt.
	// +optional
	RetryToken string `json:"retryToken,omitempty"`

	// AuditEventID is the deterministic PowerAuditEvent name for this mutation.
	// +optional
	AuditEventID string `json:"auditEventID,omitempty"`

	// AuditPhase is empty while the mutation outcome is unresolved, Pending
	// after acceptance, and Recorded once the audit event exists.
	// +optional
	// +kubebuilder:validation:Enum=Pending;Recorded
	AuditPhase string `json:"auditPhase,omitempty"`

	// AuditAction and AuditRuleName preserve the accepted decision semantics so
	// a later reconciliation does not attribute it to a newer winning rule.
	// +optional
	AuditAction string `json:"auditAction,omitempty"`
	// +optional
	AuditRuleName string `json:"auditRuleName,omitempty"`

	// NotificationSuppressed is captured from the live namespace before the
	// workload mutation and remains fixed for every retry of this action.
	// +optional
	NotificationSuppressed bool `json:"notificationSuppressed"`

	// NotificationSuppressionSource identifies the authority used for a true
	// decision. The only supported authority is the live Namespace label.
	// +optional
	// +kubebuilder:validation:Enum=namespace-label;resolution-error
	NotificationSuppressionSource string `json:"notificationSuppressionSource,omitempty"`

	// NotificationSuppressionNamespaceUID binds the decision to the Namespace
	// incarnation read directly from the Kubernetes API.
	// +optional
	NotificationSuppressionNamespaceUID string `json:"notificationSuppressionNamespaceUID,omitempty"`
}

// ObservedStateSpec captures the workload's current state.
type ObservedStateSpec struct {
	Replicas  int32 `json:"replicas"`
	Suspended bool  `json:"suspended,omitempty"`
	// ActiveJobs reports Jobs already started by a CronJob. Suspending the
	// CronJob prevents future scheduling but does not stop these Jobs.
	// +optional
	ActiveJobs int32  `json:"activeJobs,omitempty"`
	PowerState string `json:"powerState,omitempty"` // "on" or "off"
}

// RuleReference identifies a rule that participated in a decision.
type RuleReference struct {
	Kind        string `json:"kind"` // PowerPolicy or PowerOverride
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Priority    int32  `json:"priority"`
	Description string `json:"description,omitempty"`
}

// BlockReasonSpec explains a guardrail block.
type BlockReasonSpec struct {
	Type     string `json:"type"`
	Message  string `json:"message"`
	Waivable bool   `json:"waivable"`
}

// SnapshotSpec captures the state needed for restoration.
type SnapshotSpec struct {
	Available    bool         `json:"available"`
	ReplicaCount *int32       `json:"replicaCount,omitempty"`
	Suspended    *bool        `json:"suspended,omitempty"`
	Resources    ResourceSpec `json:"resources,omitempty"`
	CapturedAt   *metav1.Time `json:"capturedAt,omitempty"`
	// ResourceVersion is the workload revision observed during capture.
	// Power-down is conditional on this value to prevent stale restoration.
	// +optional
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// ResourceSpec captures resource requests for savings calculation.
type ResourceSpec struct {
	CPUMillicores int64 `json:"cpuMillicores,omitempty"`
	MemoryMiB     int64 `json:"memoryMiB,omitempty"`
}

// OwnershipSpec describes external ownership of the workload.
type OwnershipSpec struct {
	Type    string `json:"type"` // ArgoCD, Flux, Helm, HPA
	OptedIn bool   `json:"optedIn"`
}

// SavingsSpec holds accumulated savings metrics.
type SavingsSpec struct {
	CPUHoursSaved  float64 `json:"cpuHoursSaved,omitempty"`
	MemoryGiBHours float64 `json:"memoryGiBHoursSaved,omitempty"`
	EstimatedCost  float64 `json:"estimatedCost,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pt
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetRef.namespace`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=`.spec.targetRef.kind`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.desiredState`
// +kubebuilder:printcolumn:name="Observed",type=string,JSONPath=`.status.observedState.powerState`
// +kubebuilder:printcolumn:name="Blocked",type=boolean,JSONPath=`.status.blocked`
// +kubebuilder:printcolumn:name="Divergent",type=boolean,JSONPath=`.status.divergent`

// PowerTarget represents a workload under Aura Power management.
type PowerTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PowerTargetSpec   `json:"spec,omitempty"`
	Status PowerTargetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PowerTargetList contains a list of PowerTarget.
type PowerTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PowerTarget `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PowerTarget{}, &PowerTargetList{})
}
