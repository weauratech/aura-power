package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerOverrideSpec defines the desired state of PowerOverride.
type PowerOverrideSpec struct {
	// Scope defines which workloads this override targets.
	Scope PolicyScope `json:"scope"`

	// State is the desired power state during the override.
	// +kubebuilder:validation:Enum=on;off
	State string `json:"state"`

	// Priority determines which rule wins in conflicts (higher wins).
	// +kubebuilder:validation:Minimum=0
	Priority int32 `json:"priority"`

	// ExpiresAt is the mandatory expiration timestamp. Overrides without expiration are rejected.
	ExpiresAt metav1.Time `json:"expiresAt"`

	// Reason is a mandatory human-readable justification.
	// +kubebuilder:validation:MinLength=3
	Reason string `json:"reason"`

	// Reference is an optional external ticket/incident link.
	// +optional
	Reference string `json:"reference,omitempty"`
}

// PowerOverrideStatus defines the observed state of PowerOverride.
type PowerOverrideStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase indicates the override lifecycle state.
	// +kubebuilder:validation:Enum=Active;Expired
	Phase string `json:"phase,omitempty"`

	// AffectedTargets is the count of targets governed by this override.
	AffectedTargets int32 `json:"affectedTargets,omitempty"`

	// ExpiresIn is a human-readable duration until expiration.
	ExpiresIn string `json:"expiresIn,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=po
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.spec.state`
// +kubebuilder:printcolumn:name="Priority",type=integer,JSONPath=`.spec.priority`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Expires",type=string,JSONPath=`.status.expiresIn`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.spec.reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PowerOverride defines a temporary exception to power policies.
type PowerOverride struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PowerOverrideSpec   `json:"spec,omitempty"`
	Status PowerOverrideStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PowerOverrideList contains a list of PowerOverride.
type PowerOverrideList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PowerOverride `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PowerOverride{}, &PowerOverrideList{})
}
