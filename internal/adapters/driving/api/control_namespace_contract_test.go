package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestControlNamespaceScopesAPIReadsAndWrites(t *testing.T) {
	controlTarget := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "control-target", Namespace: "aura-system"}}
	foreignTarget := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "foreign-target", Namespace: "foreign"}}
	controlPolicy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "control-policy", Namespace: "aura-system"}}
	foreignPolicy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "foreign-policy", Namespace: "foreign"}}
	controlOverride := &v1alpha1.PowerOverride{ObjectMeta: metav1.ObjectMeta{Name: "control-override", Namespace: "aura-system"}}
	foreignOverride := &v1alpha1.PowerOverride{ObjectMeta: metav1.ObjectMeta{Name: "foreign-override", Namespace: "foreign"}}
	controlGroup := &v1alpha1.PowerNamespaceGroup{ObjectMeta: metav1.ObjectMeta{Name: "control-group", Namespace: "aura-system"}}
	foreignGroup := &v1alpha1.PowerNamespaceGroup{ObjectMeta: metav1.ObjectMeta{Name: "foreign-group", Namespace: "foreign"}}
	controlChannel := &v1alpha1.PowerNotificationChannel{ObjectMeta: metav1.ObjectMeta{Name: "control-channel", Namespace: "aura-system"}}
	foreignChannel := &v1alpha1.PowerNotificationChannel{ObjectMeta: metav1.ObjectMeta{Name: "foreign-channel", Namespace: "foreign"}}
	f := newContractFixture(t, controlTarget, foreignTarget, controlPolicy, foreignPolicy, controlOverride, foreignOverride, controlGroup, foreignGroup, controlChannel, foreignChannel)
	member := f.token(t, auth.RoleMember)
	admin := f.token(t, auth.RoleAdmin)

	for _, path := range []string{"/api/v1/targets", "/api/v1/policies", "/api/v1/overrides", "/api/v1/namespace-groups", "/api/v1/notification-channels"} {
		response := requestContract(t, f.server.Handler(), http.MethodGet, path, member, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), `"name":"foreign-`) {
			t.Fatalf("GET %s exposed a foreign control object: %s", path, response.Body.String())
		}
	}

	for _, resource := range []struct {
		path string
		body client.Object
	}{
		{"policies", &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "foreign"}}},
		{"overrides", &v1alpha1.PowerOverride{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "foreign"}}},
		{"namespace-groups", &v1alpha1.PowerNamespaceGroup{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "foreign"}}},
		{"notification-channels", &v1alpha1.PowerNotificationChannel{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "foreign"}}},
	} {
		response := requestContract(t, f.server.Handler(), http.MethodPost, "/api/v1/"+resource.path, admin, resource.body)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("POST %s foreign namespace: got %d want 422: %s", resource.path, response.Code, response.Body.String())
		}
	}

	for _, request := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/policies/foreign/foreign-policy"},
		{http.MethodDelete, "/api/v1/policies/foreign/foreign-policy"},
		{http.MethodDelete, "/api/v1/overrides/foreign/foreign-override"},
		{http.MethodDelete, "/api/v1/namespace-groups/foreign/foreign-group"},
		{http.MethodPut, "/api/v1/notification-channels/foreign/foreign-channel"},
		{http.MethodDelete, "/api/v1/notification-channels/foreign/foreign-channel"},
	} {
		response := requestContract(t, f.server.Handler(), request.method, request.path, admin, map[string]any{})
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s %s: got %d want 422: %s", request.method, request.path, response.Code, response.Body.String())
		}
	}

	for _, path := range []string{"/api/v1/preview/policy?namespace=foreign", "/api/v1/preview/override?namespace=foreign"} {
		response := requestContract(t, f.server.Handler(), http.MethodPost, path, member, map[string]any{})
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("POST %s: got %d want 422: %s", path, response.Code, response.Body.String())
		}
	}

	for _, object := range []client.Object{foreignPolicy, foreignOverride, foreignGroup, foreignChannel} {
		if err := f.client.Get(context.Background(), client.ObjectKeyFromObject(object), object.DeepCopyObject().(client.Object)); err != nil {
			t.Fatalf("foreign object changed or disappeared: %v", err)
		}
	}
}

func TestPendingChangeRejectsForeignControlNamespace(t *testing.T) {
	f := newContractFixture(t)
	response := requestContract(t, f.server.Handler(), http.MethodPost, "/api/v1/pending", f.token(t, auth.RoleMember), map[string]any{
		"action": "create", "resourceKind": "PowerPolicy", "resourceNamespace": "foreign", "resourceName": "policy",
		"payload": map[string]any{"metadata": map[string]any{"name": "policy", "namespace": "foreign"}, "spec": map[string]any{}},
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("foreign pending change: got %d want 422: %s", response.Code, response.Body.String())
	}
}
