package quality

import (
	"os"
	"path/filepath"
	"testing"

	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

func TestNamespaceGroupsSchemaParity(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"powerpolicies", "poweroverrides"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			chart := readCRD(t, filepath.Join("..", "..", "charts", "aura-power", "crds", name+".yaml"))
			config := readCRD(t, filepath.Join("..", "..", "config", "crd", "bases", "power.aura.sh_"+name+".yaml"))

			for source, crd := range map[string]extensionsv1.CustomResourceDefinition{"chart": chart, "config": config} {
				schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
				field := schema.Properties["spec"].Properties["scope"].Properties["namespaceGroups"]
				if field.Type != "array" || field.Items == nil || field.Items.Schema == nil || field.Items.Schema.Type != "string" {
					t.Fatalf("%s namespaceGroups schema = %#v, want array of strings", source, field)
				}
			}
		})
	}
}

func TestNotificationSuppressionSchemaParity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path func(*extensionsv1.JSONSchemaProps) extensionsv1.JSONSchemaProps
	}{
		{name: "powerauditevents", path: func(root *extensionsv1.JSONSchemaProps) extensionsv1.JSONSchemaProps {
			return root.Properties["spec"]
		}},
		{name: "powertargets", path: func(root *extensionsv1.JSONSchemaProps) extensionsv1.JSONSchemaProps {
			return root.Properties["status"].Properties["action"]
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chart := readCRD(t, filepath.Join("..", "..", "charts", "aura-power", "crds", tt.name+".yaml"))
			config := readCRD(t, filepath.Join("..", "..", "config", "crd", "bases", "power.aura.sh_"+tt.name+".yaml"))
			for source, crd := range map[string]extensionsv1.CustomResourceDefinition{"chart": chart, "config": config} {
				fields := tt.path(crd.Spec.Versions[0].Schema.OpenAPIV3Schema).Properties
				if fields["notificationSuppressed"].Type != "boolean" {
					t.Fatalf("%s notificationSuppressed schema=%#v, want boolean", source, fields["notificationSuppressed"])
				}
				sourceField := fields["notificationSuppressionSource"]
				if sourceField.Type != "string" || len(sourceField.Enum) != 1 || string(sourceField.Enum[0].Raw) != `"namespace-label"` {
					t.Fatalf("%s notificationSuppressionSource schema=%#v", source, sourceField)
				}
				if fields["notificationSuppressionNamespaceUID"].Type != "string" {
					t.Fatalf("%s notificationSuppressionNamespaceUID schema=%#v", source, fields["notificationSuppressionNamespaceUID"])
				}
				if len(tt.path(crd.Spec.Versions[0].Schema.OpenAPIV3Schema).XValidations) != 2 {
					t.Fatalf("%s suppression schema lacks cross-field validation", source)
				}
			}
		})
	}
}

func readCRD(t *testing.T, path string) extensionsv1.CustomResourceDefinition {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var crd extensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(b, &crd); err != nil {
		t.Fatal(err)
	}
	if len(crd.Spec.Versions) == 0 || crd.Spec.Versions[0].Schema == nil {
		t.Fatalf("%s has no versioned OpenAPI schema", path)
	}
	return crd
}
