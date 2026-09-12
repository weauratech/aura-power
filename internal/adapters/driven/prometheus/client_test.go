package prometheus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestQueryRangeEncodesPromQLAndParsesSamples(t *testing.T) {
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("path=%s", r.URL.Path)
		}
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "matrix",
				"result": []any{map[string]any{
					"metric": map[string]string{"job": "fixture"},
					"values": []any{[]any{1700000000.0, "1.25"}, []any{1700000060.0, "2.5"}},
				}},
			},
		})
	}))
	defer server.Close()

	client := NewClient(Config{URL: server.URL, BearerToken: "test-token"})
	start := time.Unix(1700000000, 0)
	samples, err := client.queryRange(context.Background(), `sum(rate(metric{namespace="team/a"}[5m]))`, start, start.Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].Value != 1.25 || samples[1].Timestamp.Unix() != 1700000060 {
		t.Fatalf("unexpected samples: %+v", samples)
	}
	if query.Get("query") != `sum(rate(metric{namespace="team/a"}[5m]))` || query.Get("step") != "1m0s" {
		t.Fatalf("query parameters were not preserved: %v", query)
	}
}

func TestPrometheusReportsProtocolFailures(t *testing.T) {
	tests := []struct {
		name string
		code int
		body string
	}{
		{name: "http", code: http.StatusBadGateway, body: "upstream unavailable"},
		{name: "api", code: http.StatusOK, body: `{"status":"error","error":"bad query"}`},
		{name: "json", code: http.StatusOK, body: `{`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := NewClient(Config{URL: server.URL})
			if _, err := client.queryRange(context.Background(), "up", time.Now().Add(-time.Minute), time.Now(), time.Minute); err == nil {
				t.Fatal("provider failure was reported as success")
			}
		})
	}
}

func TestPrometheusAvailabilityUsesConfiguredAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/-/healthy" || r.Header.Get("Authorization") != "Bearer fixture" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if !NewClient(Config{URL: server.URL, BearerToken: "fixture"}).IsAvailable(context.Background()) {
		t.Fatal("healthy authenticated Prometheus was unavailable")
	}
}
