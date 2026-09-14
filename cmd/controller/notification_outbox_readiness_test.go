package main

import (
	"net/http/httptest"
	"testing"

	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNotificationOutboxReadinessRequiresFullDurableContract(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*extensionsv1.CustomResourceDefinition)
		wantReady bool
	}{
		{name: "complete", wantReady: true},
		{name: "not served", mutate: func(crd *extensionsv1.CustomResourceDefinition) { crd.Spec.Versions[0].Served = false }},
		{name: "no status", mutate: func(crd *extensionsv1.CustomResourceDefinition) { crd.Spec.Versions[0].Subresources = nil }},
		{name: "mutable spec", mutate: func(crd *extensionsv1.CustomResourceDefinition) {
			crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"] = schemaWithoutValidation(crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"])
		}},
		{name: "unrelated validation", mutate: func(crd *extensionsv1.CustomResourceDefinition) {
			spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
			spec.XValidations = extensionsv1.ValidationRules{{Rule: "has(self.idempotencyKey)"}}
			crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"] = spec
		}},
		{name: "wrong phase enum", mutate: func(crd *extensionsv1.CustomResourceDefinition) {
			status := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["status"]
			phase := status.Properties["phase"]
			phase.Enum[0] = extensionsv1.JSON{Raw: []byte(`"Unknown"`)}
			status.Properties["phase"] = phase
			crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["status"] = status
		}},
		{name: "missing required", mutate: func(crd *extensionsv1.CustomResourceDefinition) {
			spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
			spec.Required = spec.Required[1:]
			crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"] = spec
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := runtime.NewScheme()
			if err := extensionsv1.AddToScheme(s); err != nil {
				t.Fatal(err)
			}
			crd := outboxReadinessCRD()
			if tc.mutate != nil {
				tc.mutate(crd)
			}
			check := notificationOutboxSchemaCheck(fake.NewClientBuilder().WithScheme(s).WithObjects(crd).Build())
			err := check(httptest.NewRequest("GET", "/readyz", nil))
			if (err == nil) != tc.wantReady {
				t.Fatalf("ready=%v err=%v", err == nil, err)
			}
		})
	}
}

func schemaWithoutValidation(spec extensionsv1.JSONSchemaProps) extensionsv1.JSONSchemaProps {
	spec.XValidations = nil
	return spec
}

func outboxReadinessCRD() *extensionsv1.CustomResourceDefinition {
	stringField := extensionsv1.JSONSchemaProps{Type: "string"}
	one, twenty := float64(1), float64(20)
	policy := extensionsv1.JSONSchemaProps{Type: "string", Enum: []extensionsv1.JSON{{Raw: []byte(`"at-most-once"`)}, {Raw: []byte(`"at-least-once"`)}}}
	phase := extensionsv1.JSONSchemaProps{Type: "string", Enum: []extensionsv1.JSON{{Raw: []byte(`"Pending"`)}, {Raw: []byte(`"InProgress"`)}, {Raw: []byte(`"Succeeded"`)}, {Raw: []byte(`"Failed"`)}, {Raw: []byte(`"Ambiguous"`)}}}
	spec := extensionsv1.JSONSchemaProps{Required: []string{"auditEvent", "channel", "event", "idempotencyKey", "deliveryPolicy", "maxAttempts"}, XValidations: extensionsv1.ValidationRules{{Rule: "self == oldSelf"}}, Properties: map[string]extensionsv1.JSONSchemaProps{
		"auditEvent": {Type: "object"}, "channel": {Type: "object"}, "event": {Type: "object"}, "idempotencyKey": stringField,
		"deliveryPolicy": policy, "maxAttempts": {Type: "integer", Minimum: &one, Maximum: &twenty},
	}}
	status := extensionsv1.JSONSchemaProps{Properties: map[string]extensionsv1.JSONSchemaProps{"phase": phase, "activeAttemptID": stringField}}
	return &extensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "powernotificationdeliveries.power.aura.sh"}, Spec: extensionsv1.CustomResourceDefinitionSpec{Versions: []extensionsv1.CustomResourceDefinitionVersion{{Name: "v1alpha1", Served: true, Storage: true, Subresources: &extensionsv1.CustomResourceSubresources{Status: &extensionsv1.CustomResourceSubresourceStatus{}}, Schema: &extensionsv1.CustomResourceValidation{OpenAPIV3Schema: &extensionsv1.JSONSchemaProps{Properties: map[string]extensionsv1.JSONSchemaProps{"spec": spec, "status": status}}}}}}}
}
