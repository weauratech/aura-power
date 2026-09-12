package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupCLIUnitTest(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	apiURL = ""
	outputFormat = ""
	namespace = ""
	httpClient = &http.Client{Timeout: time.Second}
	requestTimeout = time.Second
	t.Cleanup(func() {
		apiURL = ""
		outputFormat = ""
		namespace = ""
		httpClient = &http.Client{}
		requestTimeout = 30 * time.Second
	})
}

func TestConfigRoundTripUsesPrivateFileMode(t *testing.T) {
	setupCLIUnitTest(t)
	want := &CLIConfig{ServerURL: "https://example.test/", AccessToken: "access", RefreshToken: "refresh", Username: "member"}
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%o want=600", info.Mode().Perm())
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if *got != *want {
		t.Fatalf("round trip got=%+v want=%+v", got, want)
	}
	url, err := getServerURL()
	if err != nil || url != "https://example.test" {
		t.Fatalf("server URL=%q err=%v", url, err)
	}
}

func TestAuthenticatedRequestCarriesTokenAndBody(t *testing.T) {
	setupCLIUnitTest(t)
	var gotAuthorization, gotContentType, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "fixture"}); err != nil {
		t.Fatal(err)
	}
	response, err := authenticatedRequest(http.MethodPost, server.URL, strings.NewReader(`{"state":"off"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated || gotAuthorization != "Bearer fixture" || gotContentType != "application/json" || gotBody != `{"state":"off"}` {
		t.Fatalf("request contract: status=%d auth=%q type=%q body=%q", response.StatusCode, gotAuthorization, gotContentType, gotBody)
	}
}

func TestOverrideValidationRejectsInvalidInputBeforeNetwork(t *testing.T) {
	setupCLIUnitTest(t)
	apiURL = "http://127.0.0.1:1"
	for _, tc := range []struct {
		name     string
		state    string
		duration string
	}{
		{name: "state", state: "sleep", duration: "1h"},
		{name: "duration", state: "off", duration: "tomorrow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := runOverrideCreate("team-a/api", "Deployment", "", tc.state, tc.duration, "fixture", "", 100); err == nil {
				t.Fatal("invalid input was accepted")
			}
		})
	}
}

func TestCLICommandHTTPContracts(t *testing.T) {
	tests := []struct {
		name       string
		wantMethod string
		wantPath   string
		status     int
		response   string
		invoke     func() error
		checkBody  func(*testing.T, map[string]any)
	}{
		{name: "status", wantMethod: http.MethodGet, wantPath: "/api/v1/targets?namespace=team-a&state=off", status: 200, response: `{"targets":[],"count":0}`, invoke: func() error { namespace = "team-a"; return runStatus("off") }},
		{name: "discover", wantMethod: http.MethodGet, wantPath: "/api/v1/discover?namespace=team-a", status: 200, response: `{"totalWorkloads":0}`, invoke: func() error { namespace = "team-a"; return runDiscover() }},
		{name: "savings", wantMethod: http.MethodGet, wantPath: "/api/v1/savings?namespace=team-a&period=7d", status: 200, response: `{"totalCPUHours":0}`, invoke: func() error { namespace = "team-a"; return runSavings("7d") }},
		{name: "explain", wantMethod: http.MethodGet, wantPath: "/api/v1/targets/team-a/api/explain?kind=Deployment", status: 200, response: `{"effectiveState":"off"}`, invoke: func() error { return runExplain("team-a/api", "Deployment", "") }},
		{name: "whoami", wantMethod: http.MethodGet, wantPath: "/api/v1/auth/me", status: 200, response: `{"id":"1","username":"member","role":"member"}`, invoke: runWhoami},
		{
			name: "override", wantMethod: http.MethodPost, wantPath: "/api/v1/overrides", status: 201, response: `{"name":"override-fixture"}`,
			invoke: func() error {
				return runOverrideCreate("team-a/api", "Deployment", "uid-api", "off", "1h", "test", "ISSUE-1", 123)
			},
			checkBody: func(t *testing.T, body map[string]any) {
				spec := body["spec"].(map[string]any)
				if spec["state"] != "off" || spec["priority"] != float64(123) || spec["reason"] != "test" {
					t.Fatalf("override semantics lost: %#v", spec)
				}
				scope := spec["scope"].(map[string]any)
				refs := scope["targetRefs"].([]any)
				ref := refs[0].(map[string]any)
				if ref["namespace"] != "team-a" || ref["name"] != "api" || ref["kind"] != "Deployment" || ref["uid"] != "uid-api" {
					t.Fatalf("override target lost: %#v", scope)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupCLIUnitTest(t)
			outputFormat = "json"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.wantMethod || r.URL.RequestURI() != tc.wantPath {
					t.Errorf("request=%s %s want=%s %s", r.Method, r.URL.RequestURI(), tc.wantMethod, tc.wantPath)
				}
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Errorf("authorization=%q", r.Header.Get("Authorization"))
				}
				if tc.checkBody != nil {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					} else {
						tc.checkBody(t, body)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "fixture"}); err != nil {
				t.Fatal(err)
			}
			if err := tc.invoke(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCLIReportsRemoteAndTargetErrors(t *testing.T) {
	setupCLIUnitTest(t)
	if err := runExplain("missing-separator", "Deployment", ""); err == nil {
		t.Fatal("invalid target accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fixture failure", http.StatusBadGateway)
	}))
	defer server.Close()
	if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "fixture"}); err != nil {
		t.Fatal(err)
	}
	for name, invoke := range map[string]func() error{
		"status":   func() error { return runStatus("") },
		"discover": runDiscover,
		"savings":  func() error { return runSavings("30d") },
		"explain":  func() error { return runExplain("team-a/api", "Deployment", "") },
		"whoami":   runWhoami,
	} {
		t.Run(name, func(t *testing.T) {
			if err := invoke(); err == nil {
				t.Fatal("remote failure was reported as success")
			}
		})
	}
}

func TestHTTPStatusErrorPreservesStatusAndBody(t *testing.T) {
	setupCLIUnitTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"error":"short and stout"}`))
	}))
	defer server.Close()
	if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "fixture"}); err != nil {
		t.Fatal(err)
	}
	resp, err := authenticatedGet(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = requireStatus(resp, http.StatusOK)
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type=%T want *HTTPStatusError: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusTeapot || statusErr.Body != `{"error":"short and stout"}` {
		t.Fatalf("status error lost response details: %+v", statusErr)
	}
}

func TestRequireStatusDoesNotTruncateSuccessfulResponse(t *testing.T) {
	want := strings.Repeat("x", maxErrorBodyBytes+1024)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(want)),
	}
	got, err := requireStatus(resp, http.StatusOK)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("successful response length=%d want=%d", len(got), len(want))
	}
}

func TestRequestTimeoutIsReported(t *testing.T) {
	setupCLIUnitTest(t)
	requestTimeout = 10 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "fixture"}); err != nil {
		t.Fatal(err)
	}
	_, err := authenticatedGet(server.URL)
	if err == nil || !strings.Contains(err.Error(), "request timed out after 10ms") {
		t.Fatalf("timeout error=%v", err)
	}
}

func TestRefreshSerializesTokenAndUpdatesConfig(t *testing.T) {
	setupCLIUnitTest(t)
	want := `refresh"\\token`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			RefreshToken string `json:"refreshToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.RefreshToken != want {
			t.Errorf("refresh token=%q want=%q", request.RefreshToken, want)
		}
		_, _ = w.Write([]byte(`{"accessToken":"new-access","refreshToken":"new-refresh"}`))
	}))
	defer server.Close()
	if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "old", RefreshToken: want}); err != nil {
		t.Fatal(err)
	}
	if err := refreshTokenFlow(); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccessToken != "new-access" || cfg.RefreshToken != "new-refresh" {
		t.Fatalf("tokens not updated: %+v", cfg)
	}
}

func TestPolicyPreviewJSONSupportsSpecAndFullResource(t *testing.T) {
	for name, input := range map[string]string{
		"spec":     "scope:\n  namespaces: [team-a]\nschedule:\n  desiredState: off\n",
		"resource": "apiVersion: power.aura.sh/v1alpha1\nkind: PowerPolicy\nmetadata:\n  name: fixture\nspec:\n  scope:\n    namespaces: [team-a]\n  schedule:\n    desiredState: off\n",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := policyPreviewJSON([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("not JSON: %v: %s", err, body)
			}
			if _, hasMetadata := payload["metadata"]; hasMetadata {
				t.Fatalf("full resource metadata leaked to spec endpoint: %s", body)
			}
			schedule := payload["schedule"].(map[string]any)
			if schedule["desiredState"] != "off" {
				t.Fatalf("YAML scalar changed: %#v", schedule)
			}
		})
	}
	if _, err := policyPreviewJSON([]byte("scope: {}\n---\nscope: {}\n")); err == nil {
		t.Fatal("multi-document YAML was accepted")
	}
}

func TestEveryCLICommandRunsAgainstHTTPServer(t *testing.T) {
	policy := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(policy, []byte("scope:\n  namespaces: [team-a]\nschedule:\n  desiredState: off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
	}{
		{name: "login", args: []string{"login", "--username", "member", "--password", `p"ass`}},
		{name: "whoami", args: []string{"whoami"}},
		{name: "status", args: []string{"status", "--state", "off"}},
		{name: "discover", args: []string{"discover"}},
		{name: "explain", args: []string{"explain", "team-a/api", "--kind", "Deployment"}},
		{name: "preview", args: []string{"preview", "--file", policy}},
		{name: "override", args: []string{"override", "create", "--target", "team-a/api", "--kind", "Deployment", "--state", "off", "--duration", "1h", "--reason", "test"}},
		{name: "savings", args: []string{"savings", "--period", "7d"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupCLIUnitTest(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/auth/login":
					_, _ = w.Write([]byte(`{"accessToken":"access","refreshToken":"refresh"}`))
				case "/api/v1/auth/me":
					_, _ = w.Write([]byte(`{"id":"1","username":"member","role":"member"}`))
				case "/api/v1/targets":
					_, _ = w.Write([]byte(`{"targets":[],"count":0}`))
				case "/api/v1/discover":
					_, _ = w.Write([]byte(`{"totalWorkloads":0,"eligible":0,"blocked":0}`))
				case "/api/v1/targets/team-a/api/explain":
					_, _ = w.Write([]byte(`{"effectiveState":"off"}`))
				case "/api/v1/preview/policy":
					_, _ = w.Write([]byte(`{"totalAffected":0}`))
				case "/api/v1/overrides":
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"name":"override-fixture"}`))
				case "/api/v1/savings":
					_, _ = w.Write([]byte(`{"totalCPUHours":0}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			args := append([]string(nil), tc.args...)
			if tc.name == "login" {
				args = append(args, "--server", server.URL)
			} else if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "access", RefreshToken: "refresh"}); err != nil {
				t.Fatal(err)
			}
			root := NewRootCmd()
			root.SetArgs(args)
			root.SilenceUsage = true
			root.SilenceErrors = true
			if err := root.Execute(); err != nil {
				t.Fatalf("command %v failed: %v", args, err)
			}
		})
	}

	t.Run("logout", func(t *testing.T) {
		setupCLIUnitTest(t)
		if err := saveConfig(&CLIConfig{ServerURL: "https://example.test", AccessToken: "access", RefreshToken: "refresh"}); err != nil {
			t.Fatal(err)
		}
		root := NewRootCmd()
		root.SetArgs([]string{"logout"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig()
		if err != nil || *cfg != (CLIConfig{}) {
			t.Fatalf("logout config=%+v err=%v", cfg, err)
		}
	})
}
