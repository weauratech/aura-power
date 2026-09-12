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
