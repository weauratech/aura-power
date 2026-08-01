package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerScheduleSpec defines a named, reusable power schedule.
type PowerScheduleSpec struct {
	// Windows defines time windows when DesiredState is active.
	// +optional
	Windows []TimeWindowSpec `json:"windows,omitempty"`

	// DesiredState during windows. Outside windows, the opposite applies.
	// +kubebuilder:validation:Enum=on;off
	DesiredState string `json:"desiredState"`

	// Description is a human-readable explanation of this schedule.
	// +optional
	Description string `json:"description,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=ps
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.spec.desiredState`
// +kubebuilder:printcolumn:name="Windows",type=integer,JSONPath=`.spec.windows`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PowerSchedule defines a named, reusable power schedule referenced by namespace annotations.
type PowerSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PowerScheduleSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// PowerScheduleList contains a list of PowerSchedule.
type PowerScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PowerSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PowerSchedule{}, &PowerScheduleList{})
}
