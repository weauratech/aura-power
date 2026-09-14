//go:build load

package kubernetes

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestDiscoveryMemoryBudget is an isolated synthetic guard, not an EKS soak.
// It catches regressions back to per-namespace LISTs and unexpectedly retaining
// hundreds of MiB for a single discovery result.
func TestDiscoveryMemoryBudget(t *testing.T) {
	const workloads = 5000
	objects := make([]client.Object, 0, workloads+100)
	for i := 0; i < 100; i++ {
		objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("team-%03d", i)}})
	}
	for i := 0; i < workloads; i++ {
		objects = append(objects, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("workload-%05d", i), Namespace: fmt.Sprintf("team-%03d", i%100),
		}})
	}
	base := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(objects...).Build()
	counting := &listCountingClient{Client: base}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	got, err := NewDiscoverer(counting).DiscoverAll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if len(got) != workloads || counting.lists != 5 {
		t.Fatalf("result=%d listCalls=%d, want %d and 5", len(got), counting.lists, workloads)
	}
	const maxRetained = 96 << 20
	if delta := int64(after.HeapAlloc) - int64(before.HeapAlloc); delta > maxRetained {
		t.Fatalf("discovery retained %d bytes, budget is %d", delta, maxRetained)
	}
}
