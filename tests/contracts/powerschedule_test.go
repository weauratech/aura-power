package contracts

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

func TestPowerScheduleStateContractMatchesAPIFrontendAndCRDs(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))

	if v1alpha1.PowerScheduleStateOn != "on" || v1alpha1.PowerScheduleStateOff != "off" {
		t.Fatalf("unexpected API states: %q, %q", v1alpha1.PowerScheduleStateOn, v1alpha1.PowerScheduleStateOff)
	}

	source := desiredStateSchema(t, readYAML(t, filepath.Join(root, "config", "crd", "bases", "power.aura.sh_powerschedules.yaml")))
	chart := desiredStateSchema(t, readYAML(t, filepath.Join(root, "charts", "aura-power", "crds", "powerschedules.yaml")))
	if !reflect.DeepEqual(source, chart) {
		t.Fatalf("chart PowerSchedule state schema differs from source CRD\nsource: %#v\nchart: %#v", source, chart)
	}
	if source["type"] != "string" {
		t.Fatalf("PowerSchedule desiredState must be a string, got %#v", source["type"])
	}
	enum := mustSlice(t, source["enum"])
	if !reflect.DeepEqual(enum, []any{"on", "off"}) {
		t.Fatalf("PowerSchedule desiredState enum must be string on/off, got %#v", enum)
	}

	for _, path := range []string{
		filepath.Join(root, "config", "crd", "bases", "power.aura.sh_powerschedules.yaml"),
		filepath.Join(root, "charts", "aura-power", "crds", "powerschedules.yaml"),
	} {
		crd := readYAML(t, path)
		if !contains(scheduleSpecRequired(t, crd), "desiredState") {
			t.Fatalf("%s does not require spec.desiredState", path)
		}
	}

	goTypes := readText(t, filepath.Join(root, "api", "v1alpha1", "powerschedule_types.go"))
	if !strings.Contains(goTypes, "+kubebuilder:validation:Enum=on;off") {
		t.Fatal("Go API marker does not publish the on/off enum")
	}
	frontendTypes := readText(t, filepath.Join(root, "web", "src", "types", "index.ts"))
	if !strings.Contains(frontendTypes, "export type PowerState = 'on' | 'off';") {
		t.Fatal("frontend does not use the API on/off vocabulary")
	}
}

func desiredStateSchema(t *testing.T, crd map[string]any) map[string]any {
	t.Helper()
	spec := scheduleSpecSchema(t, crd)
	return mustMap(t, mustMap(t, spec["properties"])["desiredState"])
}

func scheduleSpecRequired(t *testing.T, crd map[string]any) []any {
	t.Helper()
	return mustSlice(t, scheduleSpecSchema(t, crd)["required"])
}

func scheduleSpecSchema(t *testing.T, crd map[string]any) map[string]any {
	t.Helper()
	versions := mustSlice(t, mustMap(t, crd["spec"])["versions"])
	schema := mustMap(t, mustMap(t, versions[0])["schema"])
	openAPI := mustMap(t, schema["openAPIV3Schema"])
	return mustMap(t, mustMap(t, openAPI["properties"])["spec"])
}

func readText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
