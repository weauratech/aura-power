package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerAuditEventSpec defines a structured audit record.
type PowerAuditEventSpec struct {
	// Timestamp of the event.
	Timestamp metav1.Time `json:"timestamp"`

	// Action identifies the type of event.
	// +kubebuilder:validation:Enum=policy.created;policy.modified;policy.deleted;override.created;override.expired;workload.powered_down;workload.restored;action.blocked;execution.error;divergence.detected;workload.opted_in
	Action string `json:"action"`

	// Actor identifies who/what triggered the event.
	Actor string `json:"actor"`

	// Target identifies the affected workload.
	Target TargetReference `json:"target"`

	// Result of the action.
	// +kubebuilder:validation:Enum=success;blocked;error
	Result string `json:"result"`

	// Reason provides context for the event.
	Reason string `json:"reason"`

	// RuleName identifies the policy/override responsible.
	// +optional
	RuleName string `json:"ruleName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=pae
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.action`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target.name`
// +kubebuilder:printcolumn:name="Result",type=string,JSONPath=`.spec.result`
// +kubebuilder:printcolumn:name="Actor",type=string,JSONPath=`.spec.actor`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PowerAuditEvent records an auditable operational event.
type PowerAuditEvent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PowerAuditEventSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// PowerAuditEventList contains a list of PowerAuditEvent.
type PowerAuditEventList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PowerAuditEvent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PowerAuditEvent{}, &PowerAuditEventList{})
}
