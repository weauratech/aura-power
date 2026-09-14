package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	NotificationDeliveryPending    = "Pending"
	NotificationDeliveryInProgress = "InProgress"
	NotificationDeliverySucceeded  = "Succeeded"
	NotificationDeliveryFailed     = "Failed"
	NotificationDeliveryAmbiguous  = "Ambiguous"
)

// NotificationObjectReference binds a delivery to the exact incarnation of a
// source audit event or channel. Name reuse can therefore never replay an old
// delivery against a replacement object.
type NotificationObjectReference struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
}

// NotificationEventSnapshot is the immutable, non-secret payload captured when
// the audit event is handed to the outbox.
type NotificationEventSnapshot struct {
	Action    string                 `json:"action"`
	Target    AuditResourceReference `json:"target"`
	Result    string                 `json:"result"`
	Reason    string                 `json:"reason"`
	RuleName  string                 `json:"ruleName,omitempty"`
	Timestamp metav1.Time            `json:"timestamp"`
}

// PowerNotificationDeliverySpec identifies exactly one audit/channel pair.
// Endpoint URLs and Secret values are deliberately excluded.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="delivery specifications are immutable"
type PowerNotificationDeliverySpec struct {
	AuditEvent     NotificationObjectReference `json:"auditEvent"`
	Channel        NotificationObjectReference `json:"channel"`
	Event          NotificationEventSnapshot   `json:"event"`
	IdempotencyKey string                      `json:"idempotencyKey"`
	// +kubebuilder:validation:Enum=at-most-once;at-least-once
	DeliveryPolicy string `json:"deliveryPolicy"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=20
	MaxAttempts int32 `json:"maxAttempts"`
}

// PowerNotificationDeliveryStatus is the durable delivery state machine.
type PowerNotificationDeliveryStatus struct {
	// +kubebuilder:validation:Enum=Pending;InProgress;Succeeded;Failed;Ambiguous
	Phase        string `json:"phase,omitempty"`
	AttemptCount int32  `json:"attemptCount,omitempty"`
	RetryCount   int32  `json:"retryCount,omitempty"`
	// PreflightFailureCount is independent from provider attempts. API reads,
	// Secret resolution, and sender selection never consume MaxAttempts.
	PreflightFailureCount int32 `json:"preflightFailureCount,omitempty"`
	// ActiveAttemptID fences a late response from an expired worker claim.
	ActiveAttemptID       string       `json:"activeAttemptID,omitempty"`
	LastAttemptID         string       `json:"lastAttemptID,omitempty"`
	StartedAt             *metav1.Time `json:"startedAt,omitempty"`
	CompletedAt           *metav1.Time `json:"completedAt,omitempty"`
	NextAttemptAt         *metav1.Time `json:"nextAttemptAt,omitempty"`
	ProviderStatusCode    int32        `json:"providerStatusCode,omitempty"`
	Response              string       `json:"response,omitempty"`
	ObservedGeneration    int64        `json:"observedGeneration,omitempty"`
	ChannelStatusRecorded bool         `json:"channelStatusRecorded,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pnd
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=`.spec.deliveryPolicy`
// +kubebuilder:printcolumn:name="Attempts",type=integer,JSONPath=`.status.attemptCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type PowerNotificationDelivery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PowerNotificationDeliverySpec   `json:"spec,omitempty"`
	Status            PowerNotificationDeliveryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type PowerNotificationDeliveryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PowerNotificationDelivery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PowerNotificationDelivery{}, &PowerNotificationDeliveryList{})
}
