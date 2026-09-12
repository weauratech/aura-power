package api

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

const auditPageSize int64 = 200

// visitAuditPages bounds each API response and temporary allocation. Callers
// can aggregate only the small result set they need or stream each page.
func visitAuditPages(ctx context.Context, c client.Client, base []client.ListOption, visit func([]v1alpha1.PowerAuditEvent) error) (int, error) {
	total := 0
	continueToken := ""
	for {
		var page v1alpha1.PowerAuditEventList
		opts := append([]client.ListOption{}, base...)
		opts = append(opts, &client.ListOptions{Raw: &metav1.ListOptions{Limit: auditPageSize, Continue: continueToken}})
		if err := c.List(ctx, &page, opts...); err != nil {
			return total, err
		}
		total += len(page.Items)
		if err := visit(page.Items); err != nil {
			return total, err
		}
		continueToken = page.Continue
		if continueToken == "" {
			return total, nil
		}
	}
}

func retainNewest(events []v1alpha1.PowerAuditEvent, incoming []v1alpha1.PowerAuditEvent, limit int) []v1alpha1.PowerAuditEvent {
	events = append(events, incoming...)
	sortAuditNewest(events)
	if len(events) > limit {
		events = events[:limit]
	}
	return events
}

func sortAuditNewest(events []v1alpha1.PowerAuditEvent) {
	// Page sizes are deliberately small, so a stable in-memory sort keeps the
	// implementation simple while the retained slice remains bounded.
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].CreationTimestamp.After(events[j-1].CreationTimestamp.Time); j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
}
