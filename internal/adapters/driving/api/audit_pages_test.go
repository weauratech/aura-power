package api

import (
	"context"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

type pagedAuditClient struct {
	client.Client
	events  []v1alpha1.PowerAuditEvent
	calls   int
	maxPage int
}

func (c *pagedAuditClient) List(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
	audits, ok := list.(*v1alpha1.PowerAuditEventList)
	if !ok {
		return c.Client.List(context.Background(), list, opts...)
	}
	var options client.ListOptions
	for _, option := range opts {
		option.ApplyToList(&options)
	}
	start, _ := strconv.Atoi(options.Raw.Continue)
	end := start + int(options.Raw.Limit)
	if end > len(c.events) {
		end = len(c.events)
	}
	audits.Items = append(audits.Items, c.events[start:end]...)
	if end < len(c.events) {
		audits.Continue = strconv.Itoa(end)
	}
	c.calls++
	if size := end - start; size > c.maxPage {
		c.maxPage = size
	}
	return nil
}

func TestAuditPaginationRetainsOnlyRequestedNewestEvents(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	events := make([]v1alpha1.PowerAuditEvent, 1001)
	for i := range events {
		events[i].ObjectMeta.CreationTimestamp = metav1.NewTime(time.Unix(int64(i), 0))
	}
	c := &pagedAuditClient{Client: base, events: events}
	newest := make([]v1alpha1.PowerAuditEvent, 0, 10)
	total, err := visitAuditPages(context.Background(), c, nil, func(page []v1alpha1.PowerAuditEvent) error {
		newest = retainNewest(newest, page, 10)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != len(events) || len(newest) != 10 || newest[0].CreationTimestamp.Time.Unix() != 1000 {
		t.Fatalf("total=%d newest=%d first=%v", total, len(newest), newest[0].CreationTimestamp)
	}
	if c.calls != 6 || c.maxPage > int(auditPageSize) {
		t.Fatalf("calls=%d maxPage=%d", c.calls, c.maxPage)
	}
}

func TestParseAuditLimitBoundsUntrustedInput(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{value: "", want: defaultAuditLimit},
		{value: "10", want: 10},
		{value: "0", want: defaultAuditLimit},
		{value: "-1", want: defaultAuditLimit},
		{value: "501", want: maxAuditLimit},
		{value: "18446744073709551615", want: defaultAuditLimit},
		{value: "invalid", want: defaultAuditLimit},
	} {
		if got := parseAuditLimit(tc.value); got != tc.want {
			t.Fatalf("parseAuditLimit(%q) = %d, want %d", tc.value, got, tc.want)
		}
	}
}
