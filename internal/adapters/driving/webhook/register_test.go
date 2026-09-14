package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

type recordingRegistrar struct {
	routes map[string]http.Handler
}

func (r *recordingRegistrar) Register(path string, handler http.Handler) {
	if r.routes == nil {
		r.routes = make(map[string]http.Handler)
	}
	r.routes[path] = handler
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	return s
}

func TestRegisterInstallsBothAdmissionRoutes(t *testing.T) {
	registrar := &recordingRegistrar{}
	register(registrar, testScheme(t))

	for _, path := range []string{ValidatePowerPolicyPath, ValidatePowerOverridePath} {
		if registrar.routes[path] == nil {
			t.Errorf("route %q was not registered", path)
		}
	}
	if len(registrar.routes) != 2 {
		t.Fatalf("registered %d routes, want 2", len(registrar.routes))
	}
}

func TestPolicyAdmissionUsesRuntimeValidation(t *testing.T) {
	decoder := admission.NewDecoder(testScheme(t))
	validator := NewPolicyValidator(decoder)
	valid := &v1alpha1.PowerPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "PowerPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: "business-hours", Namespace: "apps"},
		Spec: v1alpha1.PowerPolicySpec{
			Schedule: v1alpha1.PolicySchedule{DesiredState: "on", Windows: []v1alpha1.TimeWindowSpec{{Start: "08:00", End: "18:00", Timezone: "America/Fortaleza"}}},
		},
	}
	if response := invokeAdmission(t, validator, valid); !response.Allowed {
		t.Fatalf("valid policy denied: %s", response.Result.Message)
	}

	invalid := valid.DeepCopy()
	invalid.Spec.Schedule.Windows[0].Timezone = "Mars/Olympus"
	if response := invokeAdmission(t, validator, invalid); response.Allowed {
		t.Fatal("policy with invalid IANA timezone was allowed")
	}
}

func TestOverrideAdmissionUsesRuntimeValidation(t *testing.T) {
	decoder := admission.NewDecoder(testScheme(t))
	validator := NewOverrideValidator(decoder)
	valid := &v1alpha1.PowerOverride{
		TypeMeta:   metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "PowerOverride"},
		ObjectMeta: metav1.ObjectMeta{Name: "incident", Namespace: "apps"},
		Spec: v1alpha1.PowerOverrideSpec{
			State: "on", Priority: 100, Reason: "active incident",
			ExpiresAt: metav1.NewTime(time.Now().Add(time.Hour)),
		},
	}
	if response := invokeAdmission(t, validator, valid); !response.Allowed {
		t.Fatalf("valid override denied: %s", response.Result.Message)
	}

	invalid := valid.DeepCopy()
	invalid.Spec.ExpiresAt = metav1.NewTime(time.Now().Add(-time.Minute))
	if response := invokeAdmission(t, validator, invalid); response.Allowed {
		t.Fatal("expired override was allowed")
	}
}

func invokeAdmission(t *testing.T, handler admission.Handler, object runtime.Object) admission.Response {
	t.Helper()
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("marshal admission object: %v", err)
	}
	return handler.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID:       types.UID("test-request"),
		Operation: admissionv1.Create,
		Object:    runtime.RawExtension{Raw: raw},
	}})
}
