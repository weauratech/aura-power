package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

func (s *Server) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) handleReadyz(c *gin.Context) {
	// Check K8s API
	var list v1alpha1.PowerTargetList
	if err := s.client.List(c.Request.Context(), &list, client.Limit(1)); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "component": "kubernetes", "error": err.Error()})
		return
	}
	// Check SQLite (auth store)
	if s.authStore != nil {
		if err := s.authStore.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "component": "database", "error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func (s *Server) handleStatus(c *gin.Context) {
	ctx := c.Request.Context()

	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var policies v1alpha1.PowerPolicyList
	_ = s.client.List(ctx, &policies)

	var overrides v1alpha1.PowerOverrideList
	_ = s.client.List(ctx, &overrides)

	var poweredOn, poweredOff, blocked, divergent int
	for _, t := range targets.Items {
		switch {
		case t.Status.Blocked:
			blocked++
		case t.Status.Divergent:
			divergent++
		case t.Status.DesiredState == "off":
			poweredOff++
		default:
			poweredOn++
		}
	}

	activeOverrides := 0
	for _, o := range overrides.Items {
		if o.Status.Phase == "Active" {
			activeOverrides++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"totalTargets":    len(targets.Items),
		"poweredOn":       poweredOn,
		"poweredOff":      poweredOff,
		"blocked":         blocked,
		"divergent":       divergent,
		"activePolicies":  len(policies.Items),
		"activeOverrides": activeOverrides,
	})
}

func (s *Server) handleDashboard(c *gin.Context) {
	ctx := c.Request.Context()

	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var policies v1alpha1.PowerPolicyList
	_ = s.client.List(ctx, &policies)

	var overrides v1alpha1.PowerOverrideList
	_ = s.client.List(ctx, &overrides)

	var events v1alpha1.PowerAuditEventList
	_ = s.client.List(ctx, &events)

	// Compute stats
	var poweredOnD, poweredOffD, blockedD, divergentD, governed int
	var totalCPUSaved, totalMemSaved, totalCostSaved float64
	for _, t := range targets.Items {
		if t.Status.Managed {
			governed++
		}
		switch {
		case t.Status.Blocked:
			blockedD++
		case t.Status.Divergent:
			divergentD++
		case t.Status.ObservedState.PowerState == "off":
			poweredOffD++
		default:
			poweredOnD++
		}
		if t.Status.Savings != nil {
			totalCPUSaved += t.Status.Savings.CPUHoursSaved
			totalMemSaved += t.Status.Savings.MemoryGiBHours
			totalCostSaved += t.Status.Savings.EstimatedCost
		}
	}

	// Efficiency: governed / total
	total := len(targets.Items)
	efficiency := 0.0
	if total > 0 {
		efficiency = float64(governed) / float64(total) * 100
	}

	// Recent events (last 10)
	eventItems := events.Items
	sort.Slice(eventItems, func(i, j int) bool {
		return eventItems[i].CreationTimestamp.After(eventItems[j].CreationTimestamp.Time)
	})
	recentLimit := 10
	if len(eventItems) < recentLimit {
		recentLimit = len(eventItems)
	}
	recentEvents := make([]gin.H, 0, recentLimit)
	for _, e := range eventItems[:recentLimit] {
		recentEvents = append(recentEvents, gin.H{
			"timestamp": e.CreationTimestamp.Time,
			"action":    e.Spec.Action,
			"target":    e.Spec.Target,
			"result":    e.Spec.Result,
			"reason":    e.Spec.Reason,
			"ruleName":  e.Spec.RuleName,
		})
	}

	// Next transitions: policies with active windows, compute next transition time
	now := time.Now()
	type transition struct {
		Policy    string `json:"policy"`
		State     string `json:"state"`
		Time      string `json:"time"`
		Namespace string `json:"namespace"`
	}
	var nextTransitions []transition
	for _, p := range policies.Items {
		for _, w := range p.Spec.Schedule.Windows {
			// Simple: show next occurrence based on window end/start
			if w.Start != "" && w.End != "" {
				nextTransitions = append(nextTransitions, transition{
					Policy:    p.Name,
					State:     p.Spec.Schedule.DesiredState,
					Time:      w.End,
					Namespace: p.Namespace,
				})
			}
		}
	}
	if len(nextTransitions) > 5 {
		nextTransitions = nextTransitions[:5]
	}

	// Active overrides count
	activeOvr := 0
	for _, o := range overrides.Items {
		if o.Status.Phase == "Active" {
			activeOvr++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": gin.H{
			"totalTargets":    total,
			"poweredOn":       poweredOnD,
			"poweredOff":      poweredOffD,
			"blocked":         blockedD,
			"divergent":       divergentD,
			"governed":        governed,
			"activePolicies":  len(policies.Items),
			"activeOverrides": activeOvr,
		},
		"efficiency":      efficiency,
		"savings": gin.H{
			"cpuHours":      totalCPUSaved,
			"memoryGiBHours": totalMemSaved,
			"estimatedCost": totalCostSaved,
		},
		"recentEvents":    recentEvents,
		"nextTransitions": nextTransitions,
		"generatedAt":     now,
	})
}

func (s *Server) handleDiscover(c *gin.Context) {
	ctx := c.Request.Context()

	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	eligible, blockedCount, total := 0, 0, len(targets.Items)
	for _, t := range targets.Items {
		if t.Status.Blocked {
			blockedCount++
		} else {
			eligible++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"totalWorkloads": total,
		"eligible":       eligible,
		"blocked":        blockedCount,
	})
}

func (s *Server) handleListTargets(c *gin.Context) {
	ctx := c.Request.Context()
	namespace := c.Query("namespace")
	state := c.Query("state")

	var targets v1alpha1.PowerTargetList
	listOpts := []client.ListOption{}
	if namespace != "" {
		listOpts = append(listOpts, client.MatchingLabels{"power.aura.sh/target-namespace": namespace})
	}

	if err := s.client.List(ctx, &targets, listOpts...); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Filter by state if specified
	var filtered []v1alpha1.PowerTarget
	for _, t := range targets.Items {
		if state != "" && t.Status.DesiredState != state {
			continue
		}
		filtered = append(filtered, t)
	}

	c.JSON(http.StatusOK, gin.H{"targets": filtered, "count": len(filtered)})
}

func (s *Server) handleExplainTarget(c *gin.Context) {
	ctx := c.Request.Context()
	ns := c.Param("namespace")
	name := c.Param("name")

	// Find target by labels
	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets, client.MatchingLabels{
		"power.aura.sh/target-namespace": ns,
		"power.aura.sh/target-name":      name,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(targets.Items) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "target not found"})
		return
	}

	t := targets.Items[0]
	c.JSON(http.StatusOK, gin.H{
		"ref":             t.Spec.TargetRef,
		"effectiveState":  t.Status.DesiredState,
		"observedState":   t.Status.ObservedState,
		"winningRule":     t.Status.WinningRule,
		"suppressedRules": t.Status.SuppressedRules,
		"blocked":         t.Status.Blocked,
		"blockReasons":    t.Status.BlockReasons,
		"snapshot":        t.Status.Snapshot,
		"ownership":       t.Status.Ownership,
		"savings":         t.Status.Savings,
		"lastTransition":  t.Status.LastTransition,
	})
}

func (s *Server) handlePreviewPolicy(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse policy from request body
	var policySpec v1alpha1.PowerPolicySpec
	if err := c.ShouldBindJSON(&policySpec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy spec: " + err.Error()})
		return
	}

	// Load current state
	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var policies v1alpha1.PowerPolicyList
	if err := s.client.List(ctx, &policies); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var overrides v1alpha1.PowerOverrideList
	if err := s.client.List(ctx, &overrides); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Count affected/blocked
	affectedOff := 0
	affectedOn := 0
	blocked := 0

	for _, t := range targets.Items {
		// Simple scope match check
		matched := false
		if len(policySpec.Scope.Namespaces) == 0 {
			matched = true
		} else {
			for _, ns := range policySpec.Scope.Namespaces {
				if t.Spec.TargetRef.Namespace == ns {
					matched = true
					break
				}
			}
		}

		if !matched {
			continue
		}

		if t.Status.Blocked {
			blocked++
		} else if policySpec.Schedule.DesiredState == "off" {
			affectedOff++
		} else {
			affectedOn++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"affectedOn":    affectedOn,
		"affectedOff":   affectedOff,
		"blocked":       blocked,
		"totalAffected": affectedOn + affectedOff + blocked,
	})
}

func (s *Server) handlePreviewOverride(c *gin.Context) {
	ctx := c.Request.Context()

	var overrideSpec v1alpha1.PowerOverrideSpec
	if err := c.ShouldBindJSON(&overrideSpec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid override spec: " + err.Error()})
		return
	}

	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	affected := 0
	for _, t := range targets.Items {
		matched := false
		if len(overrideSpec.Scope.Namespaces) == 0 {
			matched = true
		} else {
			for _, ns := range overrideSpec.Scope.Namespaces {
				if t.Spec.TargetRef.Namespace == ns {
					matched = true
					break
				}
			}
		}
		if matched {
			affected++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"state":         overrideSpec.State,
		"totalAffected": affected,
	})
}

func (s *Server) handleSavings(c *gin.Context) {
	ctx := c.Request.Context()

	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var totalCPU, totalMem, totalCost float64
	for _, t := range targets.Items {
		if t.Status.Savings != nil {
			totalCPU += t.Status.Savings.CPUHoursSaved
			totalMem += t.Status.Savings.MemoryGiBHours
			totalCost += t.Status.Savings.EstimatedCost
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"totalCPUHours":      totalCPU,
		"totalMemoryGiB":     totalMem,
		"totalEstimatedCost": totalCost,
	})
}

func (s *Server) handleSavingsBreakdown(c *gin.Context) {
	ctx := c.Request.Context()

	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type workloadSavings struct {
		Namespace    string  `json:"namespace"`
		Name         string  `json:"name"`
		Kind         string  `json:"kind"`
		CPUHours     float64 `json:"cpuHours"`
		MemoryGiBH   float64 `json:"memoryGiBHours"`
		EstCost      float64 `json:"estimatedCost"`
		DesiredState string  `json:"desiredState"`
	}

	var items []workloadSavings
	namespaceTotals := map[string]float64{}

	for _, t := range targets.Items {
		if t.Status.Savings != nil && (t.Status.Savings.CPUHoursSaved > 0 || t.Status.Savings.MemoryGiBHours > 0 || t.Status.Savings.EstimatedCost > 0) {
			items = append(items, workloadSavings{
				Namespace:    t.Spec.TargetRef.Namespace,
				Name:         t.Spec.TargetRef.Name,
				Kind:         t.Spec.TargetRef.Kind,
				CPUHours:     t.Status.Savings.CPUHoursSaved,
				MemoryGiBH:   t.Status.Savings.MemoryGiBHours,
				EstCost:      t.Status.Savings.EstimatedCost,
				DesiredState: t.Status.DesiredState,
			})
			namespaceTotals[t.Spec.TargetRef.Namespace] += t.Status.Savings.EstimatedCost
		}
	}

	// Build namespace breakdown for chart
	type nsSavings struct {
		Namespace string  `json:"namespace"`
		Cost      float64 `json:"cost"`
	}
	var byNamespace []nsSavings
	for ns, cost := range namespaceTotals {
		byNamespace = append(byNamespace, nsSavings{Namespace: ns, Cost: cost})
	}

	// Count powered-off workloads
	poweredOff := 0
	for _, t := range targets.Items {
		if t.Status.DesiredState == "off" {
			poweredOff++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"workloads":    items,
		"byNamespace":  byNamespace,
		"poweredOff":   poweredOff,
		"totalTargets": len(targets.Items),
	})
}

func (s *Server) handleSavingsExport(c *gin.Context) {
	ctx := c.Request.Context()

	var targets v1alpha1.PowerTargetList
	if err := s.client.List(ctx, &targets); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=aura-power-savings.csv")

	c.Writer.WriteString("namespace,name,kind,cpu_hours_saved,memory_gib_hours_saved,estimated_cost_usd,desired_state\n")
	for _, t := range targets.Items {
		if t.Status.Savings == nil {
			continue
		}
		s := t.Status.Savings
		if s.CPUHoursSaved == 0 && s.MemoryGiBHours == 0 && s.EstimatedCost == 0 {
			continue
		}
		line := fmt.Sprintf("%s,%s,%s,%.2f,%.2f,%.4f,%s\n",
			t.Spec.TargetRef.Namespace, t.Spec.TargetRef.Name, t.Spec.TargetRef.Kind,
			s.CPUHoursSaved, s.MemoryGiBHours, s.EstimatedCost, t.Status.DesiredState)
		c.Writer.WriteString(line)
	}
}

func (s *Server) handleAuditExport(c *gin.Context) {
	ctx := c.Request.Context()

	var events v1alpha1.PowerAuditEventList
	if err := s.client.List(ctx, &events); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Sort descending
	items := events.Items
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreationTimestamp.After(items[j].CreationTimestamp.Time)
	})

	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=aura-power-audit.csv")

	c.Writer.WriteString("timestamp,action,target_namespace,target_name,target_kind,result,reason,rule_name\n")
	for _, e := range items {
		line := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s\n",
			e.Spec.Timestamp, e.Spec.Action,
			e.Spec.Target.Namespace, e.Spec.Target.Name, e.Spec.Target.Kind,
			e.Spec.Result, csvEscape(e.Spec.Reason), e.Spec.RuleName)
		c.Writer.WriteString(line)
	}
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

func (s *Server) handleAuditList(c *gin.Context) {
	ctx := c.Request.Context()

	var events v1alpha1.PowerAuditEventList
	listOpts := []client.ListOption{}

	targetNs := c.Query("targetNamespace")
	targetName := c.Query("targetName")
	if targetNs != "" && targetName != "" {
		listOpts = append(listOpts, client.MatchingLabels{
			"power.aura.sh/target-namespace": targetNs,
			"power.aura.sh/target-name":      targetName,
		})
	}

	if err := s.client.List(ctx, &events, listOpts...); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Apply limit (default 50)
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	// Sort by creation timestamp descending (most recent first)
	items := events.Items
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreationTimestamp.After(items[j].CreationTimestamp.Time)
	})

	total := len(items)
	if limit < total {
		items = items[:limit]
	}

	c.JSON(http.StatusOK, gin.H{"events": items, "count": len(items), "total": total})
}

func (s *Server) handleListPolicies(c *gin.Context) {
	ctx := c.Request.Context()

	var policies v1alpha1.PowerPolicyList
	if err := s.client.List(ctx, &policies); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": policies.Items, "count": len(policies.Items)})
}

func (s *Server) handleListOverrides(c *gin.Context) {
	ctx := c.Request.Context()

	var overrides v1alpha1.PowerOverrideList
	if err := s.client.List(ctx, &overrides); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": overrides.Items, "count": len(overrides.Items)})
}

func (s *Server) handleCreatePolicy(c *gin.Context) {
	ctx := c.Request.Context()

	var policy v1alpha1.PowerPolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy: " + err.Error()})
		return
	}

	// Ensure namespace is set
	if policy.Namespace == "" {
		policy.Namespace = "aura-system"
	}

	if err := s.client.Create(ctx, &policy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create policy: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"name": policy.Name, "namespace": policy.Namespace})
}

func (s *Server) handleUpdatePolicy(c *gin.Context) {
	ctx := c.Request.Context()
	ns := c.Param("namespace")
	name := c.Param("name")

	// Get existing policy
	var existing v1alpha1.PowerPolicy
	key := client.ObjectKey{Namespace: ns, Name: name}
	if err := s.client.Get(ctx, key, &existing); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "policy not found"})
		return
	}

	// Parse updated spec from body
	var updated v1alpha1.PowerPolicy
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy: " + err.Error()})
		return
	}

	// Apply changes to existing (preserve metadata)
	existing.Spec = updated.Spec

	if err := s.client.Update(ctx, &existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update policy: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"name": existing.Name, "namespace": existing.Namespace, "updated": true})
}

func (s *Server) handleDeletePolicy(c *gin.Context) {
	ctx := c.Request.Context()
	ns := c.Param("namespace")
	name := c.Param("name")

	policy := &v1alpha1.PowerPolicy{}
	policy.Name = name
	policy.Namespace = ns

	if err := s.client.Delete(ctx, policy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete policy: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": name})
}

func (s *Server) handleCreateOverride(c *gin.Context) {
	ctx := c.Request.Context()

	var override v1alpha1.PowerOverride
	if err := c.ShouldBindJSON(&override); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid override: " + err.Error()})
		return
	}

	if override.Namespace == "" {
		override.Namespace = "aura-system"
	}

	if err := s.client.Create(ctx, &override); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create override: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"name": override.Name, "namespace": override.Namespace})
}

func (s *Server) handleDeleteOverride(c *gin.Context) {
	ctx := c.Request.Context()
	ns := c.Param("namespace")
	name := c.Param("name")

	override := &v1alpha1.PowerOverride{}
	override.Name = name
	override.Namespace = ns

	if err := s.client.Delete(ctx, override); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete override: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": name})
}

func (s *Server) handleListNamespaces(c *gin.Context) {
	ctx := c.Request.Context()

	var nsList corev1.NamespaceList
	if err := s.client.List(ctx, &nsList); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	names := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		names = append(names, ns.Name)
	}

	c.JSON(http.StatusOK, gin.H{"namespaces": names})
}

func (s *Server) handleListNamespaceGroups(c *gin.Context) {
	ctx := c.Request.Context()

	var groups v1alpha1.PowerNamespaceGroupList
	if err := s.client.List(ctx, &groups); err != nil {
		// If CRD doesn't exist, return empty list gracefully
		c.JSON(http.StatusOK, gin.H{"items": []interface{}{}, "count": 0})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": groups.Items, "count": len(groups.Items)})
}

func (s *Server) handleCreateNamespaceGroup(c *gin.Context) {
	ctx := c.Request.Context()

	var group v1alpha1.PowerNamespaceGroup
	if err := c.ShouldBindJSON(&group); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group: " + err.Error()})
		return
	}

	if group.Namespace == "" {
		group.Namespace = "aura-system"
	}

	if err := s.client.Create(ctx, &group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create group: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"name": group.Name, "namespace": group.Namespace})
}

func (s *Server) handleDeleteNamespaceGroup(c *gin.Context) {
	ctx := c.Request.Context()
	ns := c.Param("namespace")
	name := c.Param("name")

	group := &v1alpha1.PowerNamespaceGroup{}
	group.Name = name
	group.Namespace = ns

	if err := s.client.Delete(ctx, group); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete group: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": name})
}


func (s *Server) handleListNotificationChannels(c *gin.Context) {
	ctx := c.Request.Context()

	var channels v1alpha1.PowerNotificationChannelList
	if err := s.client.List(ctx, &channels); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": channels.Items, "count": len(channels.Items)})
}

func (s *Server) handleCreateNotificationChannel(c *gin.Context) {
	ctx := c.Request.Context()

	var channel v1alpha1.PowerNotificationChannel
	if err := c.ShouldBindJSON(&channel); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel: " + err.Error()})
		return
	}

	if channel.Namespace == "" {
		channel.Namespace = "aura-system"
	}

	if err := s.client.Create(ctx, &channel); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create channel: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"name": channel.Name, "namespace": channel.Namespace})
}

func (s *Server) handleDeleteNotificationChannel(c *gin.Context) {
	ctx := c.Request.Context()
	ns := c.Param("namespace")
	name := c.Param("name")

	channel := &v1alpha1.PowerNotificationChannel{}
	channel.Name = name
	channel.Namespace = ns

	if err := s.client.Delete(ctx, channel); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete channel: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": name})
}


func (s *Server) handleUpdateNotificationChannel(c *gin.Context) {
	ctx := c.Request.Context()
	ns := c.Param("namespace")
	name := c.Param("name")

	// Get existing
	var existing v1alpha1.PowerNotificationChannel
	key := client.ObjectKey{Namespace: ns, Name: name}
	if err := s.client.Get(ctx, key, &existing); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}

	// Bind new spec
	var update struct {
		Spec v1alpha1.PowerNotificationChannelSpec `json:"spec"`
	}
	if err := c.ShouldBindJSON(&update); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload: " + err.Error()})
		return
	}

	existing.Spec = update.Spec
	if err := s.client.Update(ctx, &existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update channel: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"name": name, "namespace": ns})
}
