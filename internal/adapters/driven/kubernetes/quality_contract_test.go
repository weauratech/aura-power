package kubernetes

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/weauratech/aura-power/internal/core/domain"
)

func qualityScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestQualityExecutorRoundTripDeploymentAndStatefulSet(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		kind   domain.WorkloadKind
		object runtime.Object
		want   int32
	}{
		{"deployment", domain.WorkloadKindDeployment, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "same", Namespace: "fixtures"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(3)}}, 3},
		{"statefulset", domain.WorkloadKindStatefulSet, &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "same", Namespace: "fixtures"}, Spec: appsv1.StatefulSetSpec{Replicas: int32Ptr(2)}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithRuntimeObjects(tc.object).Build()
			executor := NewExecutor(c)
			ref := domain.WorkloadRef{Namespace: "fixtures", Name: "same", Kind: tc.kind}
			snapshot, err := executor.CaptureSnapshot(ctx, ref)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.ReplicaCount == nil || *snapshot.ReplicaCount != tc.want {
				t.Fatalf("unexpected snapshot: %+v", snapshot)
			}
			if err := executor.PowerDown(ctx, ref); err != nil {
				t.Fatal(err)
			}
			if err := executor.Restore(ctx, ref, *snapshot); err != nil {
				t.Fatal(err)
			}
			if tc.kind == domain.WorkloadKindDeployment {
				var got appsv1.Deployment
				if err := c.Get(ctx, clientKey("fixtures", "same"), &got); err != nil {
					t.Fatal(err)
				}
				if *got.Spec.Replicas != tc.want {
					t.Fatalf("replicas = %d", *got.Spec.Replicas)
				}
			} else {
				var got appsv1.StatefulSet
				if err := c.Get(ctx, clientKey("fixtures", "same"), &got); err != nil {
					t.Fatal(err)
				}
				if *got.Spec.Replicas != tc.want {
					t.Fatalf("replicas = %d", *got.Spec.Replicas)
				}
			}
		})
	}
}

func TestQualityExecutorRefusesRecreatedWorkloadUID(t *testing.T) {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "fixtures", UID: "actual"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(2)}}
	c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(dep).Build()
	executor := NewExecutor(c)
	ref := domain.WorkloadRef{Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "stale"}
	if _, err := executor.CaptureSnapshot(context.Background(), ref); err == nil {
		t.Fatal("expected snapshot capture to reject a recreated workload UID")
	}
	if err := executor.PowerDown(context.Background(), ref); err == nil {
		t.Fatal("expected mutation to reject a recreated workload UID")
	}
}

func TestQualityDiscovererKeepsKindAndNamespaceMetadata(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fixtures", Labels: map[string]string{"environment": "test"}, Annotations: map[string]string{"owner": "quality"}}}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "same", Namespace: "fixtures", Labels: map[string]string{"app": "api"}}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(2), Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")}}}}}}}}
	ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "same", Namespace: "fixtures"}, Spec: appsv1.StatefulSetSpec{Replicas: int32Ptr(1)}}
	c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(ns, dep, ss).Build()
	got, err := NewDiscoverer(c).DiscoverAll(context.Background(), []string{"fixtures"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected both homonymous kinds, got %d", len(got))
	}
	kinds := map[domain.WorkloadKind]bool{}
	for _, workload := range got {
		kinds[workload.Ref.Kind] = true
		if workload.NamespaceLabels["environment"] != "test" || workload.NamespaceAnnotations["owner"] != "quality" {
			t.Fatalf("namespace metadata lost: %+v", workload)
		}
		if workload.Ref.Kind == domain.WorkloadKindDeployment && (workload.Resources.CPUMillicores != 200 || workload.Resources.MemoryMiB != 128) {
			t.Fatalf("replica resources not aggregated: %+v", workload.Resources)
		}
	}
	if !kinds[domain.WorkloadKindDeployment] || !kinds[domain.WorkloadKindStatefulSet] {
		t.Fatalf("kinds lost: %+v", kinds)
	}
}

type listCountingClient struct {
	client.Client
	lists int
}

func (c *listCountingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.lists++
	return c.Client.List(ctx, list, opts...)
}

func TestQualityDiscovererUsesConstantClusterListCount(t *testing.T) {
	scheme := qualityScheme(t)
	objects := make([]client.Object, 0, 80)
	for i := 0; i < 40; i++ {
		namespace := "team-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		objects = append(objects,
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}},
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: namespace}},
		)
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	counting := &listCountingClient{Client: base}

	got, err := NewDiscoverer(counting).DiscoverAll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 40 {
		t.Fatalf("discovered %d workloads, want 40", len(got))
	}
	if counting.lists != 4 {
		t.Fatalf("issued %d LIST calls, want 4 regardless of namespace count", counting.lists)
	}
}

func int32Ptr(v int32) *int32 { return &v }
func clientKey(namespace, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: namespace, Name: name}
}
