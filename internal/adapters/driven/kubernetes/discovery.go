package kubernetes

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

type Discoverer struct {
	client client.Client
}

func NewDiscoverer(c client.Client) *Discoverer {
	return &Discoverer{client: c}
}

func (d *Discoverer) DiscoverAll(ctx context.Context, namespaces []string) ([]ports.DiscoveredWorkload, error) {
	var result []ports.DiscoveredWorkload

	// Fetch all namespaces with their annotations
	var nsList corev1.NamespaceList
	if err := d.client.List(ctx, &nsList); err != nil {
		return nil, err
	}

	nsAnnotations := make(map[string]map[string]string)
	nsLabels := make(map[string]map[string]string)
	if len(namespaces) == 0 {
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
			nsAnnotations[ns.Name] = ns.Annotations
			nsLabels[ns.Name] = ns.Labels
		}
	} else {
		for _, ns := range nsList.Items {
			nsAnnotations[ns.Name] = ns.Annotations
			nsLabels[ns.Name] = ns.Labels
		}
	}

	for _, ns := range namespaces {
		workloads, err := d.DiscoverByNamespace(ctx, ns)
		if err != nil {
			return nil, err
		}
		// Enrich workloads with namespace metadata
		for i := range workloads {
			workloads[i].NamespaceAnnotations = nsAnnotations[ns]
			workloads[i].NamespaceLabels = nsLabels[ns]
		}
		result = append(result, workloads...)
	}
	return result, nil
}

func (d *Discoverer) DiscoverByNamespace(ctx context.Context, namespace string) ([]ports.DiscoveredWorkload, error) {
	var result []ports.DiscoveredWorkload

	// Discover Deployments
	var deployments appsv1.DeploymentList
	if err := d.client.List(ctx, &deployments, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	for _, dep := range deployments.Items {
		result = append(result, ports.DiscoveredWorkload{
			Ref:             domain.WorkloadRef{Namespace: namespace, Name: dep.Name, Kind: domain.WorkloadKindDeployment},
			Replicas:        ptrInt32Val(dep.Spec.Replicas),
			Annotations:     dep.Annotations,
			Labels:          dep.Labels,
			NamespaceLabels: nil, // populated by caller if needed
			Resources:       computeDeploymentResources(&dep),
		})
	}

	// Discover StatefulSets
	var statefulSets appsv1.StatefulSetList
	if err := d.client.List(ctx, &statefulSets, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	for _, ss := range statefulSets.Items {
		result = append(result, ports.DiscoveredWorkload{
			Ref:         domain.WorkloadRef{Namespace: namespace, Name: ss.Name, Kind: domain.WorkloadKindStatefulSet},
			Replicas:    ptrInt32Val(ss.Spec.Replicas),
			Annotations: ss.Annotations,
			Labels:      ss.Labels,
			Resources:   computeStatefulSetResources(&ss),
		})
	}

	// Discover CronJobs
	var cronJobs batchv1.CronJobList
	if err := d.client.List(ctx, &cronJobs, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	for _, cj := range cronJobs.Items {
		result = append(result, ports.DiscoveredWorkload{
			Ref:         domain.WorkloadRef{Namespace: namespace, Name: cj.Name, Kind: domain.WorkloadKindCronJob},
			Suspended:   ptrBoolVal(cj.Spec.Suspend),
			Annotations: cj.Annotations,
			Labels:      cj.Labels,
			Resources:   computeCronJobResources(&cj),
		})
	}

	return result, nil
}

func computeDeploymentResources(dep *appsv1.Deployment) domain.ResourceSummary {
	replicas := ptrInt32Val(dep.Spec.Replicas)
	return computePodResources(dep.Spec.Template.Spec.Containers, replicas)
}

func computeStatefulSetResources(ss *appsv1.StatefulSet) domain.ResourceSummary {
	replicas := ptrInt32Val(ss.Spec.Replicas)
	return computePodResources(ss.Spec.Template.Spec.Containers, replicas)
}

func computeCronJobResources(cj *batchv1.CronJob) domain.ResourceSummary {
	return computePodResources(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, 1)
}

func computePodResources(containers []corev1.Container, replicas int32) domain.ResourceSummary {
	var cpuMillis, memMiB int64
	for _, c := range containers {
		if req := c.Resources.Requests; req != nil {
			cpuMillis += req.Cpu().MilliValue()
			memMiB += req.Memory().Value() / (1024 * 1024)
		}
	}
	return domain.ResourceSummary{
		CPUMillicores: cpuMillis * int64(replicas),
		MemoryMiB:     memMiB * int64(replicas),
	}
}

func ptrInt32Val(p *int32) int32 {
	if p == nil {
		return 1
	}
	return *p
}

func ptrBoolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
