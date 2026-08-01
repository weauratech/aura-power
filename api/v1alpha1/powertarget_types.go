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
	// Namespace of the workload.
	Namespace string `json:"namespace"`

	// Name of the workload.
	Name string `json:"name"`

	// Kind of the workload (Deployment, StatefulSet, CronJob).
	// +kubebuilder:validation:Enum=Deployment;StatefulSet;CronJob
	Kind string `json:"kind"`
}

// PowerTargetStatus defines the observed and computed state.
type PowerTargetStatus struct {
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

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ObservedStateSpec captures the workload's current state.
type ObservedStateSpec struct {
	Replicas   int32  `json:"replicas"`
	Suspended  bool   `json:"suspended,omitempty"`
	PowerState string `json:"powerState,omitempty"` // "on" or "off"
}

// RuleReference identifies a rule that participated in a decision.
type RuleReference struct {
	Kind        string `json:"kind"`                  // PowerPolicy or PowerOverride
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
}

// ResourceSpec captures resource requests for savings calculation.
type ResourceSpec struct {
	CPUMillicores int64 `json:"cpuMillicores,omitempty"`
	MemoryMiB     int64 `json:"memoryMiB,omitempty"`
}

// OwnershipSpec describes external ownership of the workload.
type OwnershipSpec struct {
	Type    string `json:"type"`    // ArgoCD, Flux, Helm, HPA
	OptedIn bool   `json:"optedIn"`
}

// SavingsSpec holds accumulated savings metrics.
type SavingsSpec struct {
	CPUHoursSaved    float64 `json:"cpuHoursSaved,omitempty"`
	MemoryGiBHours   float64 `json:"memoryGiBHoursSaved,omitempty"`
	EstimatedCost    float64 `json:"estimatedCost,omitempty"`
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
