package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyObjectsDefaultNotificationSuppressionToDelivery(t *testing.T) {
	var audit PowerAuditEvent
	if err := json.Unmarshal([]byte(`{"spec":{"timestamp":"2026-09-14T00:00:00Z","action":"workload.powered_down","actor":"system/controller","target":{"name":"fixture","namespace":"campaign","kind":"Deployment"},"result":"success","reason":"legacy"}}`), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Spec.NotificationSuppressed || audit.Spec.NotificationSuppressionSource != "" || audit.Spec.NotificationSuppressionNamespaceUID != "" {
		t.Fatalf("legacy audit acquired suppression semantics: %+v", audit.Spec)
	}

	var target PowerTarget
	if err := json.Unmarshal([]byte(`{"status":{"action":{"desiredState":"off","phase":"Applied"}}}`), &target); err != nil {
		t.Fatal(err)
	}
	if target.Status.Action == nil || target.Status.Action.NotificationSuppressed != nil {
		t.Fatalf("legacy action acquired suppression semantics: %+v", target.Status.Action)
	}
}

func TestNewObjectsWriteExplicitNotificationSuppressionDecision(t *testing.T) {
	deliver := false
	for name, value := range map[string]any{
		"audit":  PowerAuditEvent{Spec: PowerAuditEventSpec{}},
		"target": PowerTarget{Status: PowerTargetStatus{Action: &PowerActionStatus{NotificationSuppressed: &deliver}}},
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"notificationSuppressed":false`) {
			t.Fatalf("%s omitted the explicit delivery decision: %s", name, encoded)
		}
	}
}
