package opencost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenCostSummaryAndWorkloadLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/allocation/compute" || r.URL.Query().Get("window") != "1h" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":[{"team-a/api":{"name":"team-a/api","cpuCost":1,"ramCost":2,"totalCost":3.5},"team-b":{"name":"team-b","totalCost":1.25}}]}`))
	}))
	defer server.Close()
	client := NewClient(Config{URL: server.URL})
	if !client.IsAvailable(context.Background()) {
		t.Fatal("healthy OpenCost was unavailable")
	}
	summary, err := client.GetCostSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalClusterCostPerHour != 4.75 || summary.CostByNamespace["team-a/api"] != 3.5 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	cost, err := client.GetWorkloadCostPerHour(context.Background(), "team-a", "api")
	if err != nil || cost != 3.5 {
		t.Fatalf("workload cost=%v err=%v", cost, err)
	}
}

func TestOpenCostFailureAndInvalidPayload(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{name: "upstream", code: http.StatusServiceUnavailable, body: "down"},
		{name: "invalid-json", code: http.StatusOK, body: "{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			if _, err := NewClient(Config{URL: server.URL}).GetCostSummary(context.Background()); err == nil {
				t.Fatal("provider failure was reported as success")
			}
		})
	}
}
