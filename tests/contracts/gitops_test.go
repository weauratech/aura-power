package contracts

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestArgoExamplesDelegateEveryAuraOwnedField(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	for _, name := range []string{"application.yaml", "applicationset.yaml"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, "examples", "gitops", "argocd", name))
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := yaml.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			spec := mustMap(t, object["spec"])
			if object["kind"] == "ApplicationSet" {
				spec = mustMap(t, mustMap(t, spec["template"])["spec"])
			}
			syncPolicy := mustMap(t, spec["syncPolicy"])
			options := mustSlice(t, syncPolicy["syncOptions"])
			if !contains(options, "RespectIgnoreDifferences=true") {
				t.Fatal("syncPolicy.syncOptions must include RespectIgnoreDifferences=true")
			}

			delegated := map[string]bool{}
			for _, item := range mustSlice(t, spec["ignoreDifferences"]) {
				rule := mustMap(t, item)
				for _, pointer := range mustSlice(t, rule["jsonPointers"]) {
					delegated[rule["kind"].(string)+":"+pointer.(string)] = true
				}
			}
			for _, field := range []string{"Deployment:/spec/replicas", "StatefulSet:/spec/replicas", "CronJob:/spec/suspend"} {
				if !delegated[field] {
					t.Fatalf("missing Aura field delegation %s", field)
				}
			}
		})
	}
}

func TestPowerTargetActionSchemaIsPublishedAndChartMatchesSource(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	source := readYAML(t, filepath.Join(root, "config", "crd", "bases", "power.aura.sh_powertargets.yaml"))
	chart := readYAML(t, filepath.Join(root, "charts", "aura-power", "crds", "powertargets.yaml"))
	sourceAction := actionSchema(t, source)
	chartAction := actionSchema(t, chart)
	if !reflect.DeepEqual(sourceAction, chartAction) {
		t.Fatalf("chart PowerTarget action schema differs from source CRD\nsource: %#v\nchart: %#v", sourceAction, chartAction)
	}
	properties := mustMap(t, sourceAction["properties"])
	for _, field := range []string{"desiredState", "decisionKey", "phase", "attemptedAt", "completedAt", "message", "retryToken"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("PowerTarget status.action schema omits %q", field)
		}
	}
}

func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := yaml.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func actionSchema(t *testing.T, crd map[string]any) map[string]any {
	t.Helper()
	spec := mustMap(t, crd["spec"])
	versions := mustSlice(t, spec["versions"])
	version := mustMap(t, versions[0])
	schema := mustMap(t, version["schema"])
	openAPI := mustMap(t, schema["openAPIV3Schema"])
	properties := mustMap(t, openAPI["properties"])
	status := mustMap(t, properties["status"])
	statusProperties := mustMap(t, status["properties"])
	return mustMap(t, statusProperties["action"])
}

func mustMap(t *testing.T, value any) map[string]any {
	t.Helper()
	got, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", value)
	}
	return got
}

func mustSlice(t *testing.T, value any) []any {
	t.Helper()
	got, ok := value.([]any)
	if !ok {
		t.Fatalf("expected slice, got %T", value)
	}
	return got
}

func contains(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
