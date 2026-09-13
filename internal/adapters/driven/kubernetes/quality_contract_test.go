package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/notifications"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
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
			if err := executor.PowerDown(ctx, ref, *snapshot); err != nil {
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
	if err := executor.PowerDown(context.Background(), ref, domain.Snapshot{ResourceVersion: dep.ResourceVersion}); err == nil {
		t.Fatal("expected mutation to reject a recreated workload UID")
	}
}

func TestQualityPowerDownRejectsStaleSnapshotAndPreservesConcurrentScale(t *testing.T) {
	ctx := context.Background()
	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "fixtures", UID: "uid-api"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}
	c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(dep).Build()
	executor := NewExecutor(c)
	ref := domain.WorkloadRef{Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-api"}

	stale, err := executor.CaptureSnapshot(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	var concurrent appsv1.Deployment
	if err := c.Get(ctx, client.ObjectKeyFromObject(dep), &concurrent); err != nil {
		t.Fatal(err)
	}
	five := int32(5)
	concurrent.Spec.Replicas = &five
	if err := c.Update(ctx, &concurrent); err != nil {
		t.Fatal(err)
	}

	if err := executor.PowerDown(ctx, ref, *stale); !errors.Is(err, ports.ErrSnapshotStale) {
		t.Fatalf("stale snapshot was not rejected: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(dep), &concurrent); err != nil {
		t.Fatal(err)
	}
	if concurrent.Spec.Replicas == nil || *concurrent.Spec.Replicas != five {
		t.Fatalf("stale power-down overwrote concurrent scale: %+v", concurrent.Spec.Replicas)
	}

	fresh, err := executor.CaptureSnapshot(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ReplicaCount == nil || *fresh.ReplicaCount != five || fresh.ResourceVersion == stale.ResourceVersion {
		t.Fatalf("recapture did not bind the concurrent revision: stale=%+v fresh=%+v", stale, fresh)
	}
	if err := executor.PowerDown(ctx, ref, *fresh); err != nil {
		t.Fatal(err)
	}
	if err := executor.Restore(ctx, ref, *fresh); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(dep), &concurrent); err != nil {
		t.Fatal(err)
	}
	if concurrent.Spec.Replicas == nil || *concurrent.Spec.Replicas != five {
		t.Fatalf("restore did not preserve the recaptured scale: %+v", concurrent.Spec.Replicas)
	}
}

func TestQualityPowerDownConvertsUpdateConflictToSafeRecaptureSignal(t *testing.T) {
	ctx := context.Background()
	replicas := int32(3)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api-race", Namespace: "fixtures", UID: "uid-race"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}
	base := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(dep).Build()
	raceClient := &conflictBeforeUpdateClient{Client: base}
	executor := NewExecutor(raceClient)
	ref := domain.WorkloadRef{Namespace: dep.Namespace, Name: dep.Name, Kind: domain.WorkloadKindDeployment, UID: string(dep.UID)}
	snapshot, err := executor.CaptureSnapshot(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.PowerDown(ctx, ref, *snapshot); !errors.Is(err, ports.ErrSnapshotStale) {
		t.Fatalf("resourceVersion conflict was not converted to safe recapture signal: %v", err)
	}
	var live appsv1.Deployment
	if err := base.Get(ctx, client.ObjectKeyFromObject(dep), &live); err != nil {
		t.Fatal(err)
	}
	if live.Spec.Replicas == nil || *live.Spec.Replicas != 5 {
		t.Fatalf("conflicting update did not preserve the external scale: %+v", live.Spec.Replicas)
	}
}

type conflictBeforeUpdateClient struct {
	client.Client
	injected bool
}

func (c *conflictBeforeUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if !c.injected {
		if dep, ok := obj.(*appsv1.Deployment); ok {
			c.injected = true
			var live appsv1.Deployment
			if err := c.Client.Get(ctx, client.ObjectKeyFromObject(dep), &live); err != nil {
				return err
			}
			five := int32(5)
			live.Spec.Replicas = &five
			if err := c.Client.Update(ctx, &live); err != nil {
				return err
			}
		}
	}
	return c.Client.Update(ctx, obj, opts...)
}

func TestQualityAuditNotificationCorrelationRoundTrip(t *testing.T) {
	received := make(chan map[string]interface{}, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	scheme := qualityScheme(t)
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "audit-roundtrip", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "generic", URL: receiver.URL, Enabled: true},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.PowerNotificationChannel{}).
		WithObjects(channel).Build()
	dispatcher := notifications.NewDispatcher(c, c)
	recorder := NewAuditRecorder(c, nil, "aura-system")
	recorder.SetNotifier(dispatcher)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	event := ports.AuditEvent{
		Timestamp: time.Now().UTC(), Action: ports.AuditWorkloadRestored, Actor: "quality/test",
		Target: domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-1"},
		Result: "success", Reason: "round trip", RuleName: "nightly",
	}
	if err := recorder.Record(ctx, event); err != nil {
		t.Fatal(err)
	}
	var auditEvents v1alpha1.PowerAuditEventList
	if err := c.List(ctx, &auditEvents, client.InNamespace("aura-system")); err != nil {
		t.Fatal(err)
	}
	if len(auditEvents.Items) != 1 || auditEvents.Items[0].Name == "" {
		t.Fatalf("persisted audit event missing: %+v", auditEvents.Items)
	}
	wantAuditRef := "aura-system/" + auditEvents.Items[0].Name

	var payload map[string]interface{}
	select {
	case payload = <-received:
	case <-time.After(8 * time.Second):
		t.Fatal("notification receiver did not observe audit event")
	}
	correlation, ok := payload["correlation"].(map[string]interface{})
	if !ok {
		t.Fatalf("receiver correlation missing: %+v", payload)
	}
	attemptID, _ := correlation["attemptID"].(string)
	auditRefs, _ := correlation["auditEventRefs"].([]interface{})
	if attemptID == "" || len(auditRefs) != 1 || auditRefs[0] != wantAuditRef {
		t.Fatalf("receiver correlation=%+v want audit ref %q", correlation, wantAuditRef)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		var persisted v1alpha1.PowerNotificationChannel
		if err := c.Get(ctx, client.ObjectKeyFromObject(channel), &persisted); err != nil {
			t.Fatal(err)
		}
		attempt := persisted.Status.LastAttempt
		if attempt != nil && attempt.Phase == "Succeeded" {
			if attempt.ID != attemptID || len(attempt.AuditEventRefs) != 1 || attempt.AuditEventRefs[0] != wantAuditRef || attempt.ProviderStatusCode != http.StatusNoContent {
				t.Fatalf("persisted correlation differs from receiver: %+v", attempt)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("notification status did not converge: %+v", persisted.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQualityAuditRecorderIsIdempotentForDeterministicID(t *testing.T) {
	received := make(chan struct{}, 2)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	scheme := qualityScheme(t)
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "idempotent-audit", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "generic", URL: receiver.URL, Enabled: true},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.PowerNotificationChannel{}).
		WithObjects(channel).Build()
	dispatcher := notifications.NewDispatcher(c, c)
	recorder := NewAuditRecorder(c, nil, "aura-system")
	recorder.SetNotifier(dispatcher)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)
	event := ports.AuditEvent{
		ID: "action-stable", Timestamp: time.Unix(1700000000, 0).UTC(),
		Action: ports.AuditWorkloadPoweredDown, Actor: "system/controller",
		Target: domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-1"},
		Result: "success", Reason: "Powered down by policy", RuleName: "nightly",
	}
	if err := recorder.Record(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record(ctx, event); err != nil {
		t.Fatalf("identical durable audit replay failed: %v", err)
	}
	var list v1alpha1.PowerAuditEventList
	if err := c.List(context.Background(), &list, client.InNamespace("aura-system")); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != event.ID {
		t.Fatalf("audit replay created duplicates: %+v", list.Items)
	}
	select {
	case <-received:
	case <-time.After(8 * time.Second):
		t.Fatal("notification for deterministic audit event was not delivered")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var persisted v1alpha1.PowerNotificationChannel
		if err := c.Get(ctx, client.ObjectKeyFromObject(channel), &persisted); err != nil {
			t.Fatal(err)
		}
		if persisted.Status.LastAttempt != nil && persisted.Status.LastAttempt.Phase == "Succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("successful notification attempt was not persisted: %+v", persisted.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Replaying the pending audit checkpoint after delivery must re-enqueue
	// safely and be suppressed by the persisted per-channel attempt.
	if err := recorder.Record(ctx, event); err != nil {
		t.Fatalf("post-delivery durable audit replay failed: %v", err)
	}
	select {
	case <-received:
		t.Fatal("idempotent audit replay duplicated its notification")
	case <-time.After(6 * time.Second):
	}
	event.RuleName = "different"
	if err := recorder.Record(ctx, event); err == nil {
		t.Fatal("same audit identifier accepted different semantics")
	}
}

func TestQualityCronJobRestorePreservesExactSuspendState(t *testing.T) {
	ctx := context.Background()
	for _, original := range []bool{false, true} {
		t.Run(fmt.Sprintf("suspended_%t", original), func(t *testing.T) {
			current := original
			job := &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "reports", Namespace: "fixtures", UID: "cron-uid"},
				Spec: batchv1.CronJobSpec{
					Suspend:           &current,
					ConcurrencyPolicy: batchv1.ForbidConcurrent,
				},
			}
			c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(job).Build()
			executor := NewExecutor(c)
			ref := domain.WorkloadRef{Namespace: "fixtures", Name: "reports", Kind: domain.WorkloadKindCronJob, UID: "cron-uid"}

			snapshot, err := executor.CaptureSnapshot(ctx, ref)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Suspended == nil || *snapshot.Suspended != original {
				t.Fatalf("captured suspend state = %v, want %t", snapshot.Suspended, original)
			}
			if err := executor.PowerDown(ctx, ref, *snapshot); err != nil {
				t.Fatal(err)
			}
			if err := executor.Restore(ctx, ref, *snapshot); err != nil {
				t.Fatal(err)
			}
			// A repeated reconcile must be safe and preserve the same result.
			if err := executor.Restore(ctx, ref, *snapshot); err != nil {
				t.Fatalf("repeated restore failed: %v", err)
			}

			var got batchv1.CronJob
			if err := c.Get(ctx, clientKey("fixtures", "reports"), &got); err != nil {
				t.Fatal(err)
			}
			if got.Spec.Suspend == nil || *got.Spec.Suspend != original {
				t.Fatalf("restored suspend state = %v, want %t", got.Spec.Suspend, original)
			}
			if got.Spec.ConcurrencyPolicy != batchv1.ForbidConcurrent {
				t.Fatalf("restore changed unrelated CronJob field: %s", got.Spec.ConcurrencyPolicy)
			}
		})
	}
}

func TestQualityCronJobRestoreFailsClosedWithoutSuspendSnapshot(t *testing.T) {
	suspended := true
	job := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "reports", Namespace: "fixtures"}, Spec: batchv1.CronJobSpec{Suspend: &suspended}}
	c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(job).Build()
	err := NewExecutor(c).Restore(context.Background(), domain.WorkloadRef{
		Namespace: "fixtures", Name: "reports", Kind: domain.WorkloadKindCronJob,
	}, domain.Snapshot{})
	if err == nil || !strings.Contains(err.Error(), "snapshot missing suspend state") {
		t.Fatalf("expected missing snapshot error, got %v", err)
	}
	var got batchv1.CronJob
	if getErr := c.Get(context.Background(), clientKey("fixtures", "reports"), &got); getErr != nil {
		t.Fatal(getErr)
	}
	if got.Spec.Suspend == nil || !*got.Spec.Suspend {
		t.Fatalf("CronJob was changed despite missing snapshot: %v", got.Spec.Suspend)
	}
}

func TestQualityCronJobRestoreRefusesRecreatedUID(t *testing.T) {
	suspended := true
	restoreTo := false
	job := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "reports", Namespace: "fixtures", UID: "new-uid"}, Spec: batchv1.CronJobSpec{Suspend: &suspended}}
	c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(job).Build()
	err := NewExecutor(c).Restore(context.Background(), domain.WorkloadRef{
		Namespace: "fixtures", Name: "reports", Kind: domain.WorkloadKindCronJob, UID: "old-uid",
	}, domain.Snapshot{Suspended: &restoreTo})
	if err == nil || !strings.Contains(err.Error(), "workload UID changed") {
		t.Fatalf("expected recreated UID error, got %v", err)
	}
	var got batchv1.CronJob
	if getErr := c.Get(context.Background(), clientKey("fixtures", "reports"), &got); getErr != nil {
		t.Fatal(getErr)
	}
	if got.Spec.Suspend == nil || !*got.Spec.Suspend {
		t.Fatalf("recreated CronJob was changed: %v", got.Spec.Suspend)
	}
}

func TestQualityCronJobNilSuspendDefaultsToFalseAndActiveJobsAreReported(t *testing.T) {
	job := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "reports", Namespace: "fixtures", UID: "cron-uid"},
		Status:     batchv1.CronJobStatus{Active: []corev1.ObjectReference{{Name: "reports-1"}, {Name: "reports-2"}}},
	}
	c := fake.NewClientBuilder().WithScheme(qualityScheme(t)).WithObjects(job).Build()
	executor := NewExecutor(c)
	ref := domain.WorkloadRef{Namespace: "fixtures", Name: "reports", Kind: domain.WorkloadKindCronJob, UID: "cron-uid"}
	snapshot, err := executor.CaptureSnapshot(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Suspended == nil || *snapshot.Suspended {
		t.Fatalf("nil spec.suspend must be captured as Kubernetes default false: %v", snapshot.Suspended)
	}
	workloads, err := NewDiscoverer(c).DiscoverAll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(workloads) != 1 || workloads[0].ActiveJobs != 2 {
		t.Fatalf("active Jobs were not reported separately: %+v", workloads)
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
