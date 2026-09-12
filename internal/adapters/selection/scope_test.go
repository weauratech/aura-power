package selection

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

func TestResolveScopeExpandsGroupsAsNamespaceUnion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	group := &v1alpha1.PowerNamespaceGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "non-production", Namespace: "control"},
		Spec:       v1alpha1.PowerNamespaceGroupSpec{Namespaces: []string{"dev", "staging", "dev"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(group).Build()
	got, err := ResolveScope(context.Background(), c, "control", v1alpha1.PolicyScope{
		Namespaces: []string{"qa"}, NamespaceGroups: []string{"non-production"},
		NamespaceLabels: map[string]string{"team": "platform"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Namespaces) != 3 || got.Namespaces[0] != "qa" || got.Namespaces[1] != "dev" || got.Namespaces[2] != "staging" {
		t.Fatalf("unexpected namespace union: %v", got.Namespaces)
	}
	if got.NamespaceLabels["team"] != "platform" {
		t.Fatalf("labels lost: %v", got.NamespaceLabels)
	}
}

func TestResolveScopeFailsClosedForMissingGroup(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	if _, err := ResolveScope(context.Background(), c, "control", v1alpha1.PolicyScope{NamespaceGroups: []string{"missing"}}); err == nil {
		t.Fatal("missing group must not degrade to a cluster-wide scope")
	}
}
