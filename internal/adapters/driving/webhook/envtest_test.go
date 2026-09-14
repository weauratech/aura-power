//go:build envtest

package webhook

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	crwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

func TestEnvtestRegisteredWebhooksRejectInvalidObjectsAtAPIServer(t *testing.T) {
	if testing.Short() {
		t.Skip("envtest starts a real API server")
	}

	fail := admissionv1.Fail
	none := admissionv1.SideEffectClassNone
	equivalent := admissionv1.Equivalent
	pathPolicy, pathOverride := ValidatePowerPolicyPath, ValidatePowerOverridePath
	port := int32(443)
	webhooks := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "aura-power-envtest"},
		Webhooks: []admissionv1.ValidatingWebhook{
			envtestWebhook("policy.power.aura.sh", "powerpolicies", pathPolicy, port, fail, none, equivalent),
			envtestWebhook("override.power.aura.sh", "poweroverrides", pathOverride, port, fail, none, equivalent),
		},
	}
	testEnvironment := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "charts", "aura-power", "crds")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{ValidatingWebhooks: []*admissionv1.ValidatingWebhookConfiguration{webhooks}},
	}
	config, err := testEnvironment.Start()
	if err != nil {
		t.Fatalf("start envtest (set KUBEBUILDER_ASSETS with setup-envtest): %v", err)
	}
	t.Cleanup(func() {
		if stopErr := testEnvironment.Stop(); stopErr != nil {
			t.Errorf("stop envtest: %v", stopErr)
		}
	})

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	manager, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                server.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		WebhookServer: crwebhook.NewServer(crwebhook.Options{
			Host:    testEnvironment.WebhookInstallOptions.LocalServingHost,
			Port:    testEnvironment.WebhookInstallOptions.LocalServingPort,
			CertDir: testEnvironment.WebhookInstallOptions.LocalServingCertDir,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	Register(manager.GetWebhookServer(), scheme)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		if startErr := manager.Start(ctx); startErr != nil && ctx.Err() == nil {
			t.Errorf("start webhook manager: %v", startErr)
		}
	}()
	waitForWebhook(t, manager.GetWebhookServer().StartedChecker())

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "webhook-envtest"}}
	if err := k8sClient.Create(ctx, namespace); err != nil {
		t.Fatal(err)
	}

	validPolicy := &v1alpha1.PowerPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "valid-policy", Namespace: namespace.Name},
		Spec: v1alpha1.PowerPolicySpec{Schedule: v1alpha1.PolicySchedule{
			DesiredState: "on",
			Windows:      []v1alpha1.TimeWindowSpec{{Start: "08:00", End: "18:00", Timezone: "America/Fortaleza"}},
		}},
	}
	if err := k8sClient.Create(ctx, validPolicy); err != nil {
		t.Fatalf("valid policy denied: %v", err)
	}
	invalidPolicy := validPolicy.DeepCopy()
	invalidPolicy.Name = "invalid-policy"
	invalidPolicy.ResourceVersion = ""
	invalidPolicy.UID = ""
	invalidPolicy.Spec.Schedule.Windows[0].Timezone = "Mars/Olympus"
	assertAdmissionDenied(t, k8sClient.Create(ctx, invalidPolicy), "invalid timezone")

	validOverride := &v1alpha1.PowerOverride{
		ObjectMeta: metav1.ObjectMeta{Name: "valid-override", Namespace: namespace.Name},
		Spec:       v1alpha1.PowerOverrideSpec{State: "on", Priority: 100, Reason: "active incident", ExpiresAt: metav1.NewTime(time.Now().Add(time.Hour))},
	}
	if err := k8sClient.Create(ctx, validOverride); err != nil {
		t.Fatalf("valid override denied: %v", err)
	}
	invalidOverride := validOverride.DeepCopy()
	invalidOverride.Name = "invalid-override"
	invalidOverride.ResourceVersion = ""
	invalidOverride.UID = ""
	invalidOverride.Spec.ExpiresAt = metav1.NewTime(time.Now().Add(-time.Minute))
	assertAdmissionDenied(t, k8sClient.Create(ctx, invalidOverride), "must be in the future")
}

func envtestWebhook(name, resource, path string, port int32, failure admissionv1.FailurePolicyType, sideEffects admissionv1.SideEffectClass, matchPolicy admissionv1.MatchPolicyType) admissionv1.ValidatingWebhook {
	return admissionv1.ValidatingWebhook{
		Name:                    name,
		AdmissionReviewVersions: []string{"v1"},
		SideEffects:             &sideEffects,
		FailurePolicy:           &failure,
		MatchPolicy:             &matchPolicy,
		ClientConfig: admissionv1.WebhookClientConfig{Service: &admissionv1.ServiceReference{
			Namespace: "default", Name: "unused-envtest-service", Path: &path, Port: &port,
		}},
		Rules: []admissionv1.RuleWithOperations{{
			Operations: []admissionv1.OperationType{admissionv1.Create, admissionv1.Update},
			Rule:       admissionv1.Rule{APIGroups: []string{"power.aura.sh"}, APIVersions: []string{"v1alpha1"}, Resources: []string{resource}, Scope: ptrScope(admissionv1.NamespacedScope)},
		}},
	}
}

func ptrScope(value admissionv1.ScopeType) *admissionv1.ScopeType { return &value }

func waitForWebhook(t *testing.T, checker healthz.Checker) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := checker(&http.Request{}); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("webhook server did not become ready")
}

func assertAdmissionDenied(t *testing.T, err error, message string) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid object was accepted by the API server")
	}
	if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
		t.Fatalf("expected admission denial, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), message) {
		t.Fatalf("admission denial %q does not contain %q", err, message)
	}
}
