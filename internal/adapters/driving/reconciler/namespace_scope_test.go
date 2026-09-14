package reconciler

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

func scopedReconcilerClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.PowerTarget{}, &v1alpha1.PowerPolicy{}, &v1alpha1.PowerOverride{}).
		WithObjects(objects...).Build()
}

func TestControlNamespaceExcludesForeignDecisionObjects(t *testing.T) {
	ctx := context.Background()
	controlPolicy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "control", Namespace: "control"}}
	foreignPolicy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "other"}}
	controlOverride := &v1alpha1.PowerOverride{ObjectMeta: metav1.ObjectMeta{Name: "control", Namespace: "control"}}
	foreignOverride := &v1alpha1.PowerOverride{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "other"}}
	controlTarget := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "control", Namespace: "control"}}
	foreignTarget := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "other"}}
	c := scopedReconcilerClient(t, controlPolicy, foreignPolicy, controlOverride, foreignOverride, controlTarget, foreignTarget)
	r := &TargetReconciler{Client: c, ControlNamespace: "control"}

	policies, err := r.loadPolicies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0].Name != "control" || policies[0].Namespace != "control" {
		t.Fatalf("foreign policies crossed control namespace: %+v", policies)
	}
	overrides, err := r.loadOverrides(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 1 || overrides[0].Name != "control" || overrides[0].Namespace != "control" {
		t.Fatalf("foreign overrides crossed control namespace: %+v", overrides)
	}

	policyReconciler := &PolicyReconciler{Client: c, ControlNamespace: "control"}
	count, err := policyReconciler.countAffectedTargets(ctx, controlPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("affected target count crossed control namespace: got=%d want=1", count)
	}
}

func TestControlNamespaceIgnoresForeignReconcileRequests(t *testing.T) {
	ctx := context.Background()
	expires := metav1.NewTime(time.Now().Add(-time.Hour))
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "other"}}
	override := &v1alpha1.PowerOverride{
		ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "other"},
		Spec:       v1alpha1.PowerOverrideSpec{ExpiresAt: expires},
	}
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: "other"}}
	c := scopedReconcilerClient(t, policy, override, target)
	req := func(name string) ctrl.Request {
		return ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "other", Name: name}}
	}

	if _, err := (&TargetReconciler{Client: c, ControlNamespace: "control"}).Reconcile(ctx, req("foreign")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&PolicyReconciler{Client: c, ControlNamespace: "control"}).Reconcile(ctx, req("foreign")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&OverrideReconciler{Client: c, ControlNamespace: "control"}).Reconcile(ctx, req("foreign")); err != nil {
		t.Fatal(err)
	}

	var gotOverride v1alpha1.PowerOverride
	if err := c.Get(ctx, client.ObjectKeyFromObject(override), &gotOverride); err != nil {
		t.Fatal(err)
	}
	if gotOverride.Status.Phase != "" {
		t.Fatalf("foreign override status was mutated: %+v", gotOverride.Status)
	}
}
