package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	t.Cleanup(func() {
		apiURL = ""
		outputFormat = ""
		namespace = ""
		httpClient = &http.Client{Timeout: 30 * time.Second}
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
			if err := runOverrideCreate("team-a/api", tc.state, tc.duration, "fixture", "", 100); err == nil {
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
		{name: "savings", wantMethod: http.MethodGet, wantPath: "/api/v1/savings?period=7d&namespace=team-a", status: 200, response: `{"totalCPUHours":0}`, invoke: func() error { namespace = "team-a"; return runSavings("7d") }},
		{name: "explain", wantMethod: http.MethodGet, wantPath: "/api/v1/targets/team-a/api/explain", status: 200, response: `{"effectiveState":"off"}`, invoke: func() error { return runExplain("team-a/api") }},
		{name: "whoami", wantMethod: http.MethodGet, wantPath: "/api/v1/auth/me", status: 200, response: `{"id":"1","username":"member","role":"member"}`, invoke: runWhoami},
		{
			name: "override", wantMethod: http.MethodPost, wantPath: "/api/v1/overrides", status: 201, response: `{"name":"override-fixture"}`,
			invoke: func() error { return runOverrideCreate("team-a/api", "off", "1h", "test", "ISSUE-1", 123) },
			checkBody: func(t *testing.T, body map[string]any) {
				spec := body["spec"].(map[string]any)
				if spec["state"] != "off" || spec["priority"] != float64(123) || spec["reason"] != "test" {
					t.Fatalf("override semantics lost: %#v", spec)
				}
				scope := spec["scope"].(map[string]any)
				if scope["namespaces"].([]any)[0] != "team-a" || scope["workloadNames"].([]any)[0] != "api" {
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
	if err := runExplain("missing-separator"); err == nil {
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
		"explain":  func() error { return runExplain("team-a/api") },
		"whoami":   runWhoami,
	} {
		t.Run(name, func(t *testing.T) {
			if err := invoke(); err == nil {
				t.Fatal("remote failure was reported as success")
			}
		})
	}
}
