package main

import (
	"net/http/httptest"
	"testing"

	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNotificationSuppressionReadinessValidatesPersistedSchemas(t *testing.T) {
	s := runtime.NewScheme()
	if err := extensionsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	objects := []extensionsv1.CustomResourceDefinition{
		suppressionCRD("powerauditevents.power.aura.sh", []string{"spec"}),
		suppressionCRD("powertargets.power.aura.sh", []string{"status", "action"}),
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(&objects[0], &objects[1]).Build()
	check := notificationSuppressionSchemaCheck(c)
	if err := check(httptest.NewRequest("GET", "/readyz/notification-suppression-v1", nil)); err != nil {
		t.Fatal(err)
	}

	broken := objects[0].DeepCopy()
	broken.ResourceVersion = ""
	delete(broken.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties, "notificationSuppressed")
	brokenCheck := notificationSuppressionSchemaCheck(fake.NewClientBuilder().WithScheme(s).WithObjects(broken, &objects[1]).Build())
	if err := brokenCheck(httptest.NewRequest("GET", "/", nil)); err == nil {
		t.Fatal("readiness accepted missing required CRD capability")
	}
}

func suppressionCRD(name string, path []string) extensionsv1.CustomResourceDefinition {
	fields := extensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]extensionsv1.JSONSchemaProps{
		"notificationSuppressed":              {Type: "boolean"},
		"notificationSuppressionSource":       {Type: "string", Enum: []extensionsv1.JSON{{Raw: []byte(`"namespace-label"`)}}},
		"notificationSuppressionNamespaceUID": {Type: "string"},
	}}
	for i := len(path) - 1; i >= 0; i-- {
		fields = extensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]extensionsv1.JSONSchemaProps{path[i]: fields}}
	}
	return extensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: extensionsv1.CustomResourceDefinitionSpec{Versions: []extensionsv1.CustomResourceDefinitionVersion{{Name: "v1alpha1", Storage: true, Served: true, Schema: &extensionsv1.CustomResourceValidation{OpenAPIV3Schema: &fields}}}}}
}
