package main

import (
	"fmt"
	"net/http"
	"slices"

	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

func notificationOutboxSchemaCheck(reader client.Reader) healthz.Checker {
	return func(req *http.Request) error {
		var crd extensionsv1.CustomResourceDefinition
		const name = "powernotificationdeliveries.power.aura.sh"
		if err := reader.Get(req.Context(), client.ObjectKey{Name: name}, &crd); err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		for i := range crd.Spec.Versions {
			version := &crd.Spec.Versions[i]
			if version.Name != "v1alpha1" || !version.Served || !version.Storage || version.Schema == nil || version.Schema.OpenAPIV3Schema == nil || version.Subresources == nil || version.Subresources.Status == nil {
				continue
			}
			spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
			status := version.Schema.OpenAPIV3Schema.Properties["status"]
			required := []string{"auditEvent", "channel", "event", "idempotencyKey", "deliveryPolicy", "maxAttempts"}
			for _, field := range required {
				if !slices.Contains(spec.Required, field) {
					return fmt.Errorf("%s spec.%s is not required", name, field)
				}
			}
			policy := spec.Properties["deliveryPolicy"]
			attempts := spec.Properties["maxAttempts"]
			phase := status.Properties["phase"]
			if spec.Properties["idempotencyKey"].Type != "string" || policy.Type != "string" ||
				!hasJSONEnum(policy.Enum, `"at-most-once"`) || !hasJSONEnum(policy.Enum, `"at-least-once"`) ||
				attempts.Type != "integer" || attempts.Minimum == nil || *attempts.Minimum != 1 || attempts.Maximum == nil || *attempts.Maximum != 20 ||
				phase.Type != "string" || status.Properties["activeAttemptID"].Type != "string" || !hasExactPhaseEnum(phase.Enum) || !hasImmutableSpecRule(spec.XValidations) {
				return fmt.Errorf("%s durable delivery schema is incomplete", name)
			}
			return nil
		}
		return fmt.Errorf("%s has no served v1alpha1 storage schema with status subresource", name)
	}
}

func hasExactPhaseEnum(values []extensionsv1.JSON) bool {
	want := []string{`"Pending"`, `"InProgress"`, `"Succeeded"`, `"Failed"`, `"Ambiguous"`}
	if len(values) != len(want) {
		return false
	}
	for _, value := range want {
		if !hasJSONEnum(values, value) {
			return false
		}
	}
	return true
}

func hasImmutableSpecRule(rules extensionsv1.ValidationRules) bool {
	for i := range rules {
		if rules[i].Rule == "self == oldSelf" {
			return true
		}
	}
	return false
}

func hasJSONEnum(values []extensionsv1.JSON, raw string) bool {
	for i := range values {
		if string(values[i].Raw) == raw {
			return true
		}
	}
	return false
}
