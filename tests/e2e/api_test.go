//go:build e2e

// Package e2e contains end-to-end smoke tests that run against a live Aura Power instance.
// Run with: go test -tags=e2e ./tests/e2e/ -base-url=https://power.int.weaura.tech
package e2e

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

var (
	baseURL  = flag.String("base-url", "https://power.int.weaura.tech", "Base URL of the Aura Power server")
	username = flag.String("username", "admin", "Admin username")
	password = flag.String("password", "admin123", "Admin password")
)

type testClient struct {
	http *http.Client
	base string
	t    *testing.T
}

func newClient(t *testing.T) *testClient {
	jar, _ := cookiejar.New(nil)
	return &testClient{
		http: &http.Client{Timeout: 30 * time.Second, Jar: jar},
		base: *baseURL,
		t:    t,
	}
}

func (c *testClient) request(method, path string, body interface{}) (*http.Response, []byte) {
	c.t.Helper()
	time.Sleep(500 * time.Millisecond) // Rate limit to avoid gateway overload
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, bodyReader)
	if err != nil {
		c.t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("execute request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func (c *testClient) login() {
	c.t.Helper()
	resp, body := c.request("POST", "/api/v1/auth/login", map[string]string{
		"username": *username,
		"password": *password,
	})
	if resp.StatusCode != 200 {
		c.t.Fatalf("login failed: %d — %s", resp.StatusCode, string(body))
	}
}

// ── Health ───────────────────────────────────────────────────────────────────

func TestHealthz(t *testing.T) {
	c := newClient(t)
	resp, _ := c.request("GET", "/healthz", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestReadyz(t *testing.T) {
	c := newClient(t)
	resp, body := c.request("GET", "/readyz", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestMetricsEndpoint(t *testing.T) {
	c := newClient(t)
	resp, body := c.request("GET", "/metrics", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "http_request") {
		t.Error("metrics response missing http_request metrics")
	}
}

// ── Auth ─────────────────────────────────────────────────────────────────────

func TestLoginValid(t *testing.T) {
	c := newClient(t)
	resp, _ := c.request("POST", "/api/v1/auth/login", map[string]string{
		"username": *username, "password": *password,
	})
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Should have set-cookie header
	cookies := resp.Cookies()
	found := false
	for _, ck := range cookies {
		if ck.Name == "aura_session" {
			found = true
		}
	}
	if !found {
		t.Error("login response missing aura_session cookie")
	}
}

func TestLoginInvalid(t *testing.T) {
	c := newClient(t)
	resp, _ := c.request("POST", "/api/v1/auth/login", map[string]string{
		"username": "nonexistent", "password": "wrong",
	})
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMeWithoutAuth(t *testing.T) {
	c := newClient(t)
	resp, _ := c.request("GET", "/api/v1/auth/me", nil)
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMeWithAuth(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, body := c.request("GET", "/api/v1/auth/me", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "username") {
		t.Error("response missing username field")
	}
}

func TestLogout(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("POST", "/api/v1/auth/logout", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ── Core API ─────────────────────────────────────────────────────────────────

func TestDashboard(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, body := c.request("GET", "/api/v1/dashboard", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
	var data map[string]interface{}
	json.Unmarshal(body, &data)
	if _, ok := data["efficiency"]; !ok {
		t.Error("missing efficiency field")
	}
	if _, ok := data["summary"]; !ok {
		t.Error("missing summary field")
	}
	if _, ok := data["recentEvents"]; !ok {
		t.Error("missing recentEvents field")
	}
}

func TestStatus(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, body := c.request("GET", "/api/v1/status", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var data map[string]interface{}
	json.Unmarshal(body, &data)
	if _, ok := data["totalTargets"]; !ok {
		t.Error("missing totalTargets")
	}
}

func TestTargetsList(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, body := c.request("GET", "/api/v1/targets", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var data map[string]interface{}
	json.Unmarshal(body, &data)
	if _, ok := data["targets"]; !ok {
		t.Error("missing targets field")
	}
}

func TestTargetsFilterByNamespace(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/targets?namespace=argocd", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDiscover(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, body := c.request("GET", "/api/v1/discover", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestPoliciesList(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, body := c.request("GET", "/api/v1/policies", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestOverridesList(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/overrides", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSavings(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/savings", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSavingsBreakdown(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/savings/breakdown", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuditList(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/audit", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestNamespaces(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/namespaces", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUsers(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/users", nil)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ── Metrics Provider ─────────────────────────────────────────────────────────

func TestMetricsCluster(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/metrics/cluster?range=1h", nil)
	// 200 if provider available, 503 if not — both are valid
	if resp.StatusCode != 200 && resp.StatusCode != 503 {
		t.Errorf("expected 200 or 503, got %d", resp.StatusCode)
	}
}

func TestMetricsCost(t *testing.T) {
	c := newClient(t)
	c.login()
	resp, _ := c.request("GET", "/api/v1/metrics/cost", nil)
	if resp.StatusCode != 200 && resp.StatusCode != 503 {
		t.Errorf("expected 200 or 503, got %d", resp.StatusCode)
	}
}

// ── CRUD: Policy Lifecycle ───────────────────────────────────────────────────

func TestPolicyCRUD(t *testing.T) {
	c := newClient(t)
	c.login()

	policy := map[string]interface{}{
		"metadata": map[string]string{"name": "smoke-test-policy-go", "namespace": "aura-system"},
		"spec": map[string]interface{}{
			"scope":    map[string]interface{}{"namespaces": []string{"default"}},
			"schedule": map[string]interface{}{"desiredState": "off", "windows": []map[string]interface{}{{"start": "23:00", "end": "23:30", "timezone": "UTC"}}},
			"priority": 1, "description": "Go E2E smoke test",
		},
	}

	// Create
	resp, body := c.request("POST", "/api/v1/policies", policy)
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create policy: expected 201, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify exists
	resp, body = c.request("GET", "/api/v1/policies", nil)
	if !strings.Contains(string(body), "smoke-test-policy-go") {
		t.Error("created policy not found in list")
	}

	// Delete
	resp, body = c.request("DELETE", "/api/v1/policies/aura-system/smoke-test-policy-go", nil)
	if resp.StatusCode != 200 {
		t.Errorf("delete policy: expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify gone
	resp, body = c.request("GET", "/api/v1/policies", nil)
	if strings.Contains(string(body), "smoke-test-policy-go") {
		t.Error("deleted policy still in list")
	}
}

// ── CRUD: Override Lifecycle ─────────────────────────────────────────────────

func TestOverrideCRUD(t *testing.T) {
	c := newClient(t)
	c.login()

	expires := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	override := map[string]interface{}{
		"metadata": map[string]string{"name": "smoke-test-override-go", "namespace": "aura-system"},
		"spec": map[string]interface{}{
			"scope":     map[string]interface{}{"namespaces": []string{"default"}},
			"state":     "on",
			"priority":  999,
			"expiresAt": expires,
			"reason":    "Go E2E smoke test override",
		},
	}

	// Create
	resp, body := c.request("POST", "/api/v1/overrides", override)
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create override: expected 201, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify
	resp, body = c.request("GET", "/api/v1/overrides", nil)
	if !strings.Contains(string(body), "smoke-test-override-go") {
		t.Error("created override not found in list")
	}

	// Delete
	resp, body = c.request("DELETE", "/api/v1/overrides/aura-system/smoke-test-override-go", nil)
	if resp.StatusCode != 200 {
		t.Errorf("delete override: expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}

// ── CRUD: User Lifecycle ─────────────────────────────────────────────────────

func TestUserCRUD(t *testing.T) {
	c := newClient(t)
	c.login()

	user := map[string]string{
		"username": "smoke-test-user-go",
		"password": "SmokeTestPass123!",
		"role":     "member",
	}

	// Create
	resp, body := c.request("POST", "/api/v1/users", user)
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create user: expected 201, got %d: %s", resp.StatusCode, string(body))
	}

	// Extract ID
	var created map[string]interface{}
	json.Unmarshal(body, &created)
	userID, _ := created["id"].(string)
	if userID == "" {
		t.Fatal("created user missing id field")
	}

	// Verify in list
	resp, body = c.request("GET", "/api/v1/users", nil)
	if !strings.Contains(string(body), "smoke-test-user-go") {
		t.Error("created user not in list")
	}

	// Delete
	resp, body = c.request("DELETE", fmt.Sprintf("/api/v1/users/%s", userID), nil)
	if resp.StatusCode != 200 {
		t.Errorf("delete user: expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}

// ── CRUD: Preview Policy ─────────────────────────────────────────────────────

func TestPreviewPolicy(t *testing.T) {
	c := newClient(t)
	c.login()

	preview := map[string]interface{}{
		"scope":    map[string]interface{}{"namespaces": []string{"default"}},
		"schedule": map[string]interface{}{"desiredState": "off", "windows": []map[string]interface{}{{"start": "00:00", "end": "06:00", "timezone": "UTC"}}},
		"priority": 1,
	}

	resp, body := c.request("POST", "/api/v1/preview/policy", preview)
	if resp.StatusCode != 200 {
		t.Errorf("preview policy: expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}

// ── RBAC: Member cannot create policies ──────────────────────────────────────

func TestRBACMemberCannotCreate(t *testing.T) {
	c := newClient(t)
	c.login()

	// Create a member user
	user := map[string]string{"username": "smoke-test-rbac", "password": "RbacTest123!", "role": "member"}
	resp, body := c.request("POST", "/api/v1/users", user)
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("setup: create user failed: %d: %s", resp.StatusCode, string(body))
	}
	var created map[string]interface{}
	json.Unmarshal(body, &created)
	userID, _ := created["id"].(string)

	// Login as member
	memberClient := newClient(t)
	resp, _ = memberClient.request("POST", "/api/v1/auth/login", map[string]string{
		"username": "smoke-test-rbac", "password": "RbacTest123!",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("member login failed: %d", resp.StatusCode)
	}

	// Try to create policy (should be 403)
	policy := map[string]interface{}{
		"metadata": map[string]string{"name": "smoke-test-rbac-policy", "namespace": "aura-system"},
		"spec": map[string]interface{}{
			"scope":    map[string]interface{}{"namespaces": []string{"default"}},
			"schedule": map[string]interface{}{"desiredState": "off"},
			"priority": 1,
		},
	}
	resp, _ = memberClient.request("POST", "/api/v1/policies", policy)
	if resp.StatusCode != 403 {
		t.Errorf("RBAC: member should get 403, got %d", resp.StatusCode)
	}

	// Cleanup: delete user (re-login as admin)
	c2 := newClient(t)
	c2.login()
	c2.request("DELETE", fmt.Sprintf("/api/v1/users/%s", userID), nil)
}
