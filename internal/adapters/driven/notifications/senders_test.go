package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenericSenderContract(t *testing.T) {
	event := Event{Action: "workload.powered_down", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment"}, Result: "success", Reason: "scheduled", RuleName: "nights", Timestamp: time.Unix(1700000000, 0).UTC()}
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected webhook request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	if err := (&GenericSender{}).Send(context.Background(), server.URL, event); err != nil {
		t.Fatal(err)
	}
	if payload["version"] != "1" || payload["event"] != event.Action || payload["timestamp"] != event.Timestamp.Format(time.RFC3339) {
		t.Fatalf("unexpected envelope: %#v", payload)
	}
	target := payload["target"].(map[string]any)
	if target["namespace"] != "team-a" || target["name"] != "api" || target["kind"] != "Deployment" {
		t.Fatalf("target identity lost: %#v", target)
	}
}

func TestWebhookRetriesTransientFailure(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	if err := httpPost(context.Background(), server.URL, map[string]string{"event": "fixture"}); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts=%d", attempts.Load())
	}
}
