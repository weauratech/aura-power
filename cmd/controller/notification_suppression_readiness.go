package main

import (
	"fmt"
	"net/http"

	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

func notificationSuppressionSchemaCheck(reader client.Reader) healthz.Checker {
	return func(req *http.Request) error {
		checks := []struct {
			name string
			path []string
		}{
			{name: "powerauditevents.power.aura.sh", path: []string{"spec"}},
			{name: "powertargets.power.aura.sh", path: []string{"status", "action"}},
		}
		for _, check := range checks {
			var crd extensionsv1.CustomResourceDefinition
			if err := reader.Get(req.Context(), client.ObjectKey{Name: check.name}, &crd); err != nil {
				return fmt.Errorf("read %s: %w", check.name, err)
			}
			var schema *extensionsv1.JSONSchemaProps
			for i := range crd.Spec.Versions {
				if crd.Spec.Versions[i].Storage && crd.Spec.Versions[i].Schema != nil {
					schema = crd.Spec.Versions[i].Schema.OpenAPIV3Schema
					break
				}
			}
			if schema == nil {
				return fmt.Errorf("%s has no storage schema", check.name)
			}
			current := *schema
			for _, segment := range check.path {
				next, ok := current.Properties[segment]
				if !ok {
					return fmt.Errorf("%s schema lacks %s", check.name, segment)
				}
				current = next
			}
			fields := current.Properties
			if fields["notificationSuppressed"].Type != "boolean" || fields["notificationSuppressionSource"].Type != "string" || fields["notificationSuppressionNamespaceUID"].Type != "string" {
				return fmt.Errorf("%s suppression schema is incomplete", check.name)
			}
			enums := fields["notificationSuppressionSource"].Enum
			if len(enums) != 1 || string(enums[0].Raw) != `"namespace-label"` {
				return fmt.Errorf("%s suppression source enum is incompatible", check.name)
			}
		}
		return nil
	}
}
