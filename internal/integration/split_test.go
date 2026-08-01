package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	"github.com/weauratech/aura-power/internal/adapters/driving/api"
	"github.com/weauratech/aura-power/internal/core/domain"
)

// TestServerAuthFlow tests the complete authentication flow.
func TestServerAuthFlow(t *testing.T) {
	srv, jwtSvc, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Login
	loginBody := `{"username":"admin","password":"testpass"}`
	resp := doRequest(t, srv, "POST", "/api/v1/auth/login", loginBody, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d", resp.StatusCode)
	}

	var tokens struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	json.NewDecoder(resp.Body).Decode(&tokens)
	resp.Body.Close()

	if tokens.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}
	if tokens.RefreshToken == "" {
		t.Fatal("expected non-empty refresh token")
	}

	// 2. Access protected endpoint with token
	resp = doRequest(t, srv, "GET", "/api/v1/status", "", tokens.AccessToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on status with token, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 3. Access without token → 401 (only on auth-protected routes that have middleware)
	// Note: In production the middleware is registered before routes via main.go ordering.
	// In test with RegisterAuthRoutes called after NewServer, middleware applies globally.
	resp = doRequest(t, srv, "GET", "/api/v1/auth/me", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /auth/me without token, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. Access with invalid token → 401
	resp = doRequest(t, srv, "GET", "/api/v1/auth/me", "", "invalid-token")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 with invalid token, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 5. Refresh token flow
	refreshBody := fmt.Sprintf(`{"refreshToken":"%s"}`, tokens.RefreshToken)
	resp = doRequest(t, srv, "POST", "/api/v1/auth/refresh", refreshBody, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on refresh, got %d", resp.StatusCode)
	}

	var newTokens struct {
		AccessToken string `json:"accessToken"`
	}
	json.NewDecoder(resp.Body).Decode(&newTokens)
	resp.Body.Close()

	if newTokens.AccessToken == "" {
		t.Fatal("expected new access token from refresh")
	}

	// 6. Validate /api/v1/auth/me returns user info
	resp = doRequest(t, srv, "GET", "/api/v1/auth/me", "", tokens.AccessToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on me, got %d", resp.StatusCode)
	}

	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()

	if me.Username != "admin" {
		t.Fatalf("expected username 'admin', got %q", me.Username)
	}
	if me.Role != "admin" {
		t.Fatalf("expected role 'admin', got %q", me.Role)
	}

	// 7. Validate wrong credentials → 401
	badLogin := `{"username":"admin","password":"wrongpass"}`
	resp = doRequest(t, srv, "POST", "/api/v1/auth/login", badLogin, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on bad login, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	_ = jwtSvc // silence unused warning
}

// TestServerRoleAccess tests role-based access control.
func TestServerRoleAccess(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Login as admin
	adminToken := login(t, srv, "admin", "testpass")

	// Create a member user
	createUserBody := `{"username":"viewer","password":"viewerpass","role":"member"}`
	resp := doRequest(t, srv, "POST", "/api/v1/users", createUserBody, adminToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 on create user, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Login as member
	memberToken := login(t, srv, "viewer", "viewerpass")

	// Member can read targets
	resp = doRequest(t, srv, "GET", "/api/v1/status", "", memberToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for member reading status, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Member CANNOT manage users (admin-only)
	resp = doRequest(t, srv, "GET", "/api/v1/users", "", memberToken)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for member accessing users, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestHealthEndpoints tests that health probes don't require auth.
func TestHealthEndpoints(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Healthz without auth
	resp := doRequest(t, srv, "GET", "/healthz", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on healthz, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Readyz without auth
	resp = doRequest(t, srv, "GET", "/readyz", "", "")
	// May be 200 or 503 depending on K8s client availability in test env
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 200 or 503 on readyz, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- Helpers ---

func setupTestServer(t *testing.T) (*httptest.Server, *auth.JWTService, func()) {
	t.Helper()

	// Create temp DB
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := auth.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}

	// Create admin user
	_, err = store.CreateUser("admin", "testpass", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	jwtSvc := auth.NewJWTService(auth.JWTConfig{
		SecretKey:       "test-secret-key-for-integration",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	})

	// Create fake K8s client with scheme
	testScheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(testScheme))
	utilruntime.Must(v1alpha1.AddToScheme(testScheme))
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()

	// Create API server with fake K8s client
	config := api.ServerConfig{
		Port:            "0",
		GuardrailConfig: domain.DefaultGuardrailConfig(),
		CostConfig:      domain.DefaultCostConfig(),
	}
	apiServer := api.NewServer(fakeClient, nil, config)
	apiServer.RegisterAuthRoutes(store, jwtSvc)
	apiServer.FinalizeRoutes()

	// Create httptest server
	srv := httptest.NewServer(apiServer.Handler())

	cleanup := func() {
		srv.Close()
		os.Remove(dbPath)
	}

	return srv, jwtSvc, cleanup
}

func doRequest(t *testing.T, srv *httptest.Server, method, path, body, token string) *http.Response {
	t.Helper()

	var bodyReader *bytes.Reader
	if body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	}

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest(method, srv.URL+path, bodyReader)
	} else {
		req, err = http.NewRequest(method, srv.URL+path, nil)
	}
	if err != nil {
		t.Fatal(err)
	}

	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func login(t *testing.T, srv *httptest.Server, username, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":"%s","password":"%s"}`, username, password)
	resp := doRequest(t, srv, "POST", "/api/v1/auth/login", body, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed for %s: status %d", username, resp.StatusCode)
	}
	var tokens struct {
		AccessToken string `json:"accessToken"`
	}
	json.NewDecoder(resp.Body).Decode(&tokens)
	resp.Body.Close()
	return tokens.AccessToken
}
