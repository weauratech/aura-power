package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerNotificationChannelSpec defines a webhook notification destination.
type PowerNotificationChannelSpec struct {
	// Type determines the payload format.
	// +kubebuilder:validation:Enum=google-chat;slack;discord;generic
	Type string `json:"type"`

	// URL is the webhook endpoint.
	// +optional
	URL string `json:"url,omitempty"`

	// URLFrom references a Secret key containing the webhook URL.
	// +optional
	URLFrom *SecretKeyRef `json:"urlFrom,omitempty"`

	// Events to notify on. Empty = all events.
	// +optional
	Events []string `json:"events,omitempty"`

	// NamespaceFilter limits notifications to specific namespaces. Empty = all.
	// +optional
	NamespaceFilter []string `json:"namespaceFilter,omitempty"`

	// Throttle duration between notifications for the same target.
	// +optional
	Throttle string `json:"throttle,omitempty"`

	// Enabled controls whether this channel is active.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// SecretKeyRef references a key in a Kubernetes Secret.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// PowerNotificationChannelStatus defines the observed state.
type PowerNotificationChannelStatus struct {
	// LastNotification is the last time a notification was sent successfully.
	// +optional
	LastNotification *metav1.Time `json:"lastNotification,omitempty"`

	// LastError is the last error encountered when sending.
	// +optional
	LastError string `json:"lastError,omitempty"`

	// TotalSent is the total number of notifications sent.
	TotalSent int64 `json:"totalSent,omitempty"`

	// TotalErrors is the total number of send failures.
	TotalErrors int64 `json:"totalErrors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pnc
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`
// +kubebuilder:printcolumn:name="Sent",type=integer,JSONPath=`.status.totalSent`
// +kubebuilder:printcolumn:name="Errors",type=integer,JSONPath=`.status.totalErrors`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PowerNotificationChannel defines a webhook destination for power events.
type PowerNotificationChannel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PowerNotificationChannelSpec   `json:"spec,omitempty"`
	Status PowerNotificationChannelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PowerNotificationChannelList contains a list of PowerNotificationChannel.
type PowerNotificationChannelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PowerNotificationChannel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PowerNotificationChannel{}, &PowerNotificationChannelList{})
}
