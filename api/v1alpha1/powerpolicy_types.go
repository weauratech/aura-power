package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerPolicySpec defines the desired state of PowerPolicy.
type PowerPolicySpec struct {
	// Scope defines which workloads this policy targets (AND intersection logic).
	Scope PolicyScope `json:"scope"`

	// Schedule defines when the desired state is active.
	Schedule PolicySchedule `json:"schedule"`

	// Priority determines which rule wins in conflicts (higher wins).
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	Priority int32 `json:"priority"`

	// Description is a human-readable explanation of this policy's purpose.
	// +optional
	Description string `json:"description,omitempty"`
}

// PolicyScope defines targeting criteria for workloads.
type PolicyScope struct {
	// Namespaces to target (empty = all non-system namespaces).
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// NamespaceGroups references PowerNamespaceGroup names to include.
	// +optional
	NamespaceGroups []string `json:"namespaceGroups,omitempty"`

	// NamespaceLabels selects namespaces by labels (AND with Namespaces).
	// +optional
	NamespaceLabels map[string]string `json:"namespaceLabels,omitempty"`

	// WorkloadNames targets specific workload names within matched namespaces.
	// +optional
	WorkloadNames []string `json:"workloadNames,omitempty"`

	// WorkloadLabels selects workloads by labels (AND with other selectors).
	// +optional
	WorkloadLabels map[string]string `json:"workloadLabels,omitempty"`
}

// PolicySchedule defines the time-based behavior.
type PolicySchedule struct {
	// Windows defines time windows when DesiredState is active.
	// Empty means the policy is always active (24/7).
	// +optional
	Windows []TimeWindowSpec `json:"windows,omitempty"`

	// DesiredState during windows. Outside windows, the opposite applies.
	// +kubebuilder:validation:Enum=on;off
	DesiredState string `json:"desiredState"`
}

// TimeWindowSpec defines a recurring time segment.
type TimeWindowSpec struct {
	// Start time (HH:MM format, e.g., "08:00").
	Start string `json:"start"`

	// End time (HH:MM format, e.g., "18:00"). If Start > End, wraps past midnight.
	End string `json:"end"`

	// Days of the week (0=Sunday, 6=Saturday). Empty means every day.
	// +optional
	Days []int `json:"days,omitempty"`

	// Timezone in IANA format (e.g., "America/Sao_Paulo").
	Timezone string `json:"timezone"`
}

// PowerPolicyStatus defines the observed state of PowerPolicy.
type PowerPolicyStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// AffectedTargets is the count of PowerTargets governed by this policy.
	AffectedTargets int32 `json:"affectedTargets,omitempty"`

	// ActiveWindow indicates if the policy is currently in an active window.
	ActiveWindow bool `json:"activeWindow,omitempty"`

	// NextTransition is the next time the policy will change state.
	// +optional
	NextTransition *metav1.Time `json:"nextTransition,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pp
// +kubebuilder:printcolumn:name="Priority",type=integer,JSONPath=`.spec.priority`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.spec.schedule.desiredState`
// +kubebuilder:printcolumn:name="Targets",type=integer,JSONPath=`.status.affectedTargets`
// +kubebuilder:printcolumn:name="Active",type=boolean,JSONPath=`.status.activeWindow`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PowerPolicy defines a recurring power schedule for workloads.
type PowerPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PowerPolicySpec   `json:"spec,omitempty"`
	Status PowerPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PowerPolicyList contains a list of PowerPolicy.
type PowerPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PowerPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PowerPolicy{}, &PowerPolicyList{})
}
