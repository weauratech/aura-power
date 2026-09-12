package kubernetes

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/weauratech/aura-power/internal/core/domain"
)

type Executor struct {
	client client.Client
}

func NewExecutor(c client.Client) *Executor {
	return &Executor{client: c}
}

func (e *Executor) CaptureSnapshot(ctx context.Context, ref domain.WorkloadRef) (*domain.Snapshot, error) {
	switch ref.Kind {
	case domain.WorkloadKindDeployment:
		return e.captureDeployment(ctx, ref)
	case domain.WorkloadKindStatefulSet:
		return e.captureStatefulSet(ctx, ref)
	case domain.WorkloadKindCronJob:
		return e.captureCronJob(ctx, ref)
	default:
		return nil, fmt.Errorf("unsupported workload kind: %s", ref.Kind)
	}
}

func (e *Executor) PowerDown(ctx context.Context, ref domain.WorkloadRef) error {
	switch ref.Kind {
	case domain.WorkloadKindDeployment:
		return e.powerDownDeployment(ctx, ref)
	case domain.WorkloadKindStatefulSet:
		return e.powerDownStatefulSet(ctx, ref)
	case domain.WorkloadKindCronJob:
		return e.powerDownCronJob(ctx, ref)
	default:
		return fmt.Errorf("unsupported workload kind: %s", ref.Kind)
	}
}

func (e *Executor) Restore(ctx context.Context, ref domain.WorkloadRef, snapshot domain.Snapshot) error {
	switch ref.Kind {
	case domain.WorkloadKindDeployment:
		return e.restoreDeployment(ctx, ref, snapshot)
	case domain.WorkloadKindStatefulSet:
		return e.restoreStatefulSet(ctx, ref, snapshot)
	case domain.WorkloadKindCronJob:
		return e.restoreCronJob(ctx, ref, snapshot)
	default:
		return fmt.Errorf("unsupported workload kind: %s", ref.Kind)
	}
}

func (e *Executor) captureDeployment(ctx context.Context, ref domain.WorkloadRef) (*domain.Snapshot, error) {
	var dep appsv1.Deployment
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &dep); err != nil {
		return nil, err
	}
	if err := verifyUID(ref, string(dep.UID)); err != nil {
		return nil, err
	}
	replicas := ptrInt32Val(dep.Spec.Replicas)
	return &domain.Snapshot{
		ReplicaCount: &replicas,
		Resources:    computeDeploymentResources(&dep),
	}, nil
}

func (e *Executor) powerDownDeployment(ctx context.Context, ref domain.WorkloadRef) error {
	var dep appsv1.Deployment
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &dep); err != nil {
		return err
	}
	if err := verifyUID(ref, string(dep.UID)); err != nil {
		return err
	}
	zero := int32(0)
	dep.Spec.Replicas = &zero
	if err := e.client.Update(ctx, &dep); err != nil {
		return fmt.Errorf("failed to scale down deployment %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	return nil
}

func (e *Executor) captureStatefulSet(ctx context.Context, ref domain.WorkloadRef) (*domain.Snapshot, error) {
	var ss appsv1.StatefulSet
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &ss); err != nil {
		return nil, err
	}
	if err := verifyUID(ref, string(ss.UID)); err != nil {
		return nil, err
	}
	replicas := ptrInt32Val(ss.Spec.Replicas)
	return &domain.Snapshot{
		ReplicaCount: &replicas,
		Resources:    computeStatefulSetResources(&ss),
	}, nil
}

func (e *Executor) powerDownStatefulSet(ctx context.Context, ref domain.WorkloadRef) error {
	var ss appsv1.StatefulSet
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &ss); err != nil {
		return err
	}
	if err := verifyUID(ref, string(ss.UID)); err != nil {
		return err
	}
	zero := int32(0)
	ss.Spec.Replicas = &zero
	if err := e.client.Update(ctx, &ss); err != nil {
		return fmt.Errorf("failed to scale down statefulset %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	return nil
}

func (e *Executor) captureCronJob(ctx context.Context, ref domain.WorkloadRef) (*domain.Snapshot, error) {
	var cj batchv1.CronJob
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &cj); err != nil {
		return nil, err
	}
	if err := verifyUID(ref, string(cj.UID)); err != nil {
		return nil, err
	}

	suspended := ptrBoolVal(cj.Spec.Suspend)
	return &domain.Snapshot{
		Suspended: &suspended,
		Resources: computeCronJobResources(&cj),
	}, nil
}

func (e *Executor) powerDownCronJob(ctx context.Context, ref domain.WorkloadRef) error {
	var cj batchv1.CronJob
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &cj); err != nil {
		return err
	}
	if err := verifyUID(ref, string(cj.UID)); err != nil {
		return err
	}
	trueBool := true
	cj.Spec.Suspend = &trueBool
	if err := e.client.Update(ctx, &cj); err != nil {
		return fmt.Errorf("failed to suspend cronjob %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	return nil
}

func (e *Executor) restoreDeployment(ctx context.Context, ref domain.WorkloadRef, snapshot domain.Snapshot) error {
	if snapshot.ReplicaCount == nil {
		return fmt.Errorf("snapshot missing replica count for deployment %s/%s", ref.Namespace, ref.Name)
	}

	var dep appsv1.Deployment
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &dep); err != nil {
		return err
	}
	if err := verifyUID(ref, string(dep.UID)); err != nil {
		return err
	}

	dep.Spec.Replicas = snapshot.ReplicaCount
	return e.client.Update(ctx, &dep)
}

func (e *Executor) restoreStatefulSet(ctx context.Context, ref domain.WorkloadRef, snapshot domain.Snapshot) error {
	if snapshot.ReplicaCount == nil {
		return fmt.Errorf("snapshot missing replica count for statefulset %s/%s", ref.Namespace, ref.Name)
	}

	var ss appsv1.StatefulSet
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &ss); err != nil {
		return err
	}
	if err := verifyUID(ref, string(ss.UID)); err != nil {
		return err
	}

	ss.Spec.Replicas = snapshot.ReplicaCount
	return e.client.Update(ctx, &ss)
}

func (e *Executor) restoreCronJob(ctx context.Context, ref domain.WorkloadRef, snapshot domain.Snapshot) error {
	var cj batchv1.CronJob
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &cj); err != nil {
		return err
	}
	if err := verifyUID(ref, string(cj.UID)); err != nil {
		return err
	}

	falseBool := false
	cj.Spec.Suspend = &falseBool
	return e.client.Update(ctx, &cj)
}

func verifyUID(ref domain.WorkloadRef, actual string) error {
	if ref.UID != "" && ref.UID != actual {
		return fmt.Errorf("workload UID changed for %s %s/%s: expected %s, got %s", ref.Kind, ref.Namespace, ref.Name, ref.UID, actual)
	}
	return nil
}
