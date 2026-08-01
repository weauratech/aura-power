package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerNamespaceGroupSpec defines a named group of namespaces.
type PowerNamespaceGroupSpec struct {
	// Namespaces is the list of namespace names in this group.
	Namespaces []string `json:"namespaces"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=png
// +kubebuilder:printcolumn:name="Namespaces",type=string,JSONPath=`.spec.namespaces`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PowerNamespaceGroup defines a named group of namespaces for use in policies.
type PowerNamespaceGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PowerNamespaceGroupSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// PowerNamespaceGroupList contains a list of PowerNamespaceGroup.
type PowerNamespaceGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PowerNamespaceGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PowerNamespaceGroup{}, &PowerNamespaceGroupList{})
}
