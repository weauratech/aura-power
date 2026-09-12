//go:build acceptance

package kubernetes

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

func TestAcceptanceCTRL01CronJobRestorePreservesOriginallySuspended(t *testing.T) {
	ctx := context.Background()
	suspended := true
	job := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "reports", Namespace: "fixtures"}, Spec: batchv1.CronJobSpec{Suspend: &suspended}}
	c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(job).Build()
	executor := NewExecutor(c)
	ref := domain.WorkloadRef{Namespace: "fixtures", Name: "reports", Kind: domain.WorkloadKindCronJob}
	snapshot, err := executor.CaptureSnapshot(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.PowerDown(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := executor.Restore(ctx, ref, *snapshot); err != nil {
		t.Fatal(err)
	}
	var got batchv1.CronJob
	if err := c.Get(ctx, clientKey("fixtures", "reports"), &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Suspend == nil || !*got.Spec.Suspend {
		t.Fatalf("CTRL-01: originally suspended CronJob was re-enabled: suspend=%v", got.Spec.Suspend)
	}
}

func TestAcceptanceCTRL07ExecutionErrorsAreNotifiable(t *testing.T) {
	if !isNotifiableAction(string(ports.AuditExecutionError)) {
		t.Fatalf("CTRL-07: %q audit events never reach notification dispatch", ports.AuditExecutionError)
	}
}

func TestAcceptanceCTRL08AuditTargetFilterIncludesKind(t *testing.T) {
	s := qualityScheme(t)
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(s).Build()
	recorder := NewAuditRecorder(c, nil, "aura-system")
	ctx := context.Background()
	for _, kind := range []domain.WorkloadKind{domain.WorkloadKindDeployment, domain.WorkloadKindStatefulSet} {
		if err := recorder.Record(ctx, ports.AuditEvent{Timestamp: time.Now(), Action: ports.AuditWorkloadPoweredDown, Target: domain.WorkloadRef{Namespace: "fixtures", Name: "same", Kind: kind}, Result: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	target := domain.WorkloadRef{Namespace: "fixtures", Name: "same", Kind: domain.WorkloadKindDeployment}
	got, err := recorder.List(ctx, ports.AuditListOptions{Target: &target})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Target.Kind != domain.WorkloadKindDeployment {
		t.Fatalf("CTRL-08: audit target filter conflates workload kinds: %+v", got)
	}
}

func TestAcceptanceCTRL02ResourceFallbackIsPerResource(t *testing.T) {
	containers := []corev1.Container{{Resources: corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
	}}}
	got := computePodResources(containers, 1)
	if got.CPUMillicores != 100 || got.MemoryMiB != 256 {
		t.Fatalf("CTRL-02: expected request/limit fallback per resource, got %+v", got)
	}
}
