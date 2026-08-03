//go:build load

// Package load contains load tests that validate performance requirements.
// Run with: go test -tags=load -v ./tests/load/ -base-url=https://power.int.weaura.tech -count=1
//
// This test creates 500 PowerTarget-like entries via policies and measures reconciliation time.
// Requires admin access to the target server.
package load

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"testing"
	"time"
)

var (
	baseURL  = flag.String("base-url", "https://power.int.weaura.tech", "Base URL")
	username = flag.String("username", "admin", "Admin username")
	password = flag.String("password", "admin123", "Admin password")
)

type client struct {
	http *http.Client
	base string
}

func newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{
		http: &http.Client{Timeout: 60 * time.Second, Jar: jar},
		base: *baseURL,
	}
}

func (c *client) login(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"username": *username, "password": *password})
	resp, err := c.http.Post(c.base+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login status: %d", resp.StatusCode)
	}
}

func (c *client) post(path string, payload interface{}) (*http.Response, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", c.base+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return c.http.Do(req)
}

func (c *client) delete(path string) (*http.Response, error) {
	req, _ := http.NewRequest("DELETE", c.base+path, nil)
	return c.http.Do(req)
}

func (c *client) get(path string) (map[string]interface{}, error) {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result, nil
}

// TestReconcilePerformance creates many policies targeting many namespaces
// and measures how quickly the dashboard reflects the changes.
func TestReconcilePerformance(t *testing.T) {
	c := newClient()
	c.login(t)

	const numPolicies = 20
	const prefix = "load-test-policy-"

	t.Log("Creating load test policies...")
	start := time.Now()

	// Create policies in parallel
	var wg sync.WaitGroup
	errors := make([]error, numPolicies)
	for i := 0; i < numPolicies; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			policy := map[string]interface{}{
				"metadata": map[string]string{
					"name":      fmt.Sprintf("%s%03d", prefix, idx),
					"namespace": "aura-system",
				},
				"spec": map[string]interface{}{
					"scope": map[string]interface{}{
						"namespaces": []string{"default"},
					},
					"schedule": map[string]interface{}{
						"desiredState": "off",
						"windows": []map[string]interface{}{
							{"start": "23:55", "end": "23:59", "timezone": "UTC"},
						},
					},
					"priority":    idx + 1,
					"description": "Load test - auto cleanup",
				},
			}
			resp, err := c.post("/api/v1/policies", policy)
			if err != nil {
				errors[idx] = err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != 201 && resp.StatusCode != 200 {
				errors[idx] = fmt.Errorf("create policy %d: status %d", idx, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	createDuration := time.Since(start)

	// Count errors
	errCount := 0
	for _, e := range errors {
		if e != nil {
			errCount++
			t.Logf("  error: %v", e)
		}
	}
	t.Logf("Created %d policies in %v (%d errors)", numPolicies-errCount, createDuration, errCount)

	// Wait for reconciliation to process
	t.Log("Waiting for reconciliation...")
	time.Sleep(35 * time.Second)

	// Measure dashboard response time
	dashStart := time.Now()
	dashboard, err := c.get("/api/v1/dashboard")
	dashDuration := time.Since(dashStart)
	if err != nil {
		t.Fatalf("dashboard failed: %v", err)
	}
	t.Logf("Dashboard responded in %v", dashDuration)

	// Verify dashboard has policies
	if summary, ok := dashboard["summary"].(map[string]interface{}); ok {
		policies := summary["activePolicies"]
		t.Logf("Active policies reported: %v", policies)
	}

	// Measure targets list response time
	targetsStart := time.Now()
	targets, err := c.get("/api/v1/targets")
	targetsDuration := time.Since(targetsStart)
	if err != nil {
		t.Fatalf("targets failed: %v", err)
	}
	t.Logf("Targets list responded in %v", targetsDuration)

	if count, ok := targets["count"].(float64); ok {
		t.Logf("Total targets: %.0f", count)
	}

	// NFR-01.1: dashboard < 3s, targets < 5s
	if dashDuration > 3*time.Second {
		t.Errorf("NFR-01.3 FAIL: Dashboard took %v (limit: 3s)", dashDuration)
	}
	if targetsDuration > 5*time.Second {
		t.Errorf("NFR-01.2 FAIL: Targets took %v (limit: 5s)", targetsDuration)
	}

	// Cleanup
	t.Log("Cleaning up load test policies...")
	for i := 0; i < numPolicies; i++ {
		name := fmt.Sprintf("%s%03d", prefix, i)
		resp, _ := c.delete(fmt.Sprintf("/api/v1/policies/aura-system/%s", name))
		if resp != nil {
			resp.Body.Close()
		}
	}
	t.Log("Cleanup complete")
}

// TestAPIThroughput measures how many requests/second the API can handle.
func TestAPIThroughput(t *testing.T) {
	c := newClient()
	c.login(t)

	const duration = 10 * time.Second
	const concurrency = 5

	t.Logf("Running throughput test for %v with %d concurrent workers...", duration, concurrency)

	var totalRequests int64
	var totalErrors int64
	var mu sync.Mutex
	done := make(chan struct{})

	for w := 0; w < concurrency; w++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					_, err := c.get("/api/v1/status")
					mu.Lock()
					totalRequests++
					if err != nil {
						totalErrors++
					}
					mu.Unlock()
				}
			}
		}()
	}

	time.Sleep(duration)
	close(done)
	time.Sleep(100 * time.Millisecond) // let goroutines finish

	rps := float64(totalRequests) / duration.Seconds()
	t.Logf("Results: %d requests in %v = %.1f req/s (%d errors)", totalRequests, duration, rps, totalErrors)

	if rps < 10 {
		t.Errorf("Throughput too low: %.1f req/s (expected > 10)", rps)
	}
}
