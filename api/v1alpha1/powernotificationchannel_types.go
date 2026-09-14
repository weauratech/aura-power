package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerNotificationChannelSpec defines a webhook notification destination.
type PowerNotificationChannelSpec struct {
	// Type determines the payload format.
	// Discord is retained for backward-compatible CRD upgrades. New UI flows do
	// not advertise it until a Discord-specific payload adapter is implemented.
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

	// LastAttempt is the durable correlation record for the latest delivery.
	// It intentionally contains identifiers and provider metadata, never the
	// destination URL, response body, credentials, or Secret contents.
	// +optional
	LastAttempt *NotificationAttemptStatus `json:"lastAttempt,omitempty"`

	// RecentAttempts retains a bounded history for operational correlation.
	// +optional
	// +kubebuilder:validation:MaxItems=20
	RecentAttempts []NotificationAttemptStatus `json:"recentAttempts,omitempty"`
}

// NotificationAttemptStatus correlates source audit events with one provider
// delivery attempt and its sanitized outcome.
type NotificationAttemptStatus struct {
	ID string `json:"id"`

	// EventIDs are stable hashes for every event included in the delivery.
	EventIDs []string `json:"eventIDs"`

	// AuditEventRefs are namespace/name references to the source audit records.
	// +optional
	AuditEventRefs []string `json:"auditEventRefs,omitempty"`

	// Phase is InProgress, Succeeded, or Failed.
	// +kubebuilder:validation:Enum=InProgress;Succeeded;Failed
	Phase string `json:"phase"`

	StartedAt metav1.Time `json:"startedAt"`

	// CompletedAt is set after the provider responds or delivery fails.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// ProviderStatusCode is the HTTP response status when available.
	// +optional
	ProviderStatusCode int32 `json:"providerStatusCode,omitempty"`

	// AttemptCount is the number of HTTP attempts made by the sender.
	AttemptCount int32 `json:"attemptCount"`

	// Response records only a sanitized outcome (for example "accepted").
	Response string `json:"response,omitempty"`
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
