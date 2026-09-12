//go:build acceptance

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func isolatedCLI(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	apiURL = ""
	outputFormat = "json"
	namespace = ""
	httpClient = &http.Client{}
	requestTimeout = 30 * time.Second
	t.Cleanup(func() {
		apiURL = ""
		outputFormat = ""
		namespace = ""
		httpClient = &http.Client{}
		requestTimeout = 30 * time.Second
	})
}

func TestAcceptanceLoginSerializesCredentialsAsJSON(t *testing.T) {
	isolatedCLI(t)
	var decoded map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"access","refreshToken":"refresh"}`))
	}))
	defer server.Close()
	username := `user"name`
	password := `p\\ass"word`
	if err := runLogin(server.URL, username, password); err != nil {
		t.Fatalf("valid credential characters were not JSON encoded: %v", err)
	}
	if decoded["username"] != username || decoded["password"] != password {
		t.Fatalf("credentials changed in transit: %#v", decoded)
	}
}

func TestAcceptancePreviewConvertsYAMLToJSON(t *testing.T) {
	isolatedCLI(t)
	var contentType string
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		body, _ = io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalAffected":0}`))
	}))
	defer server.Close()

	if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "access", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(policy, []byte("scope:\n  namespaces: [acceptance]\nschedule:\n  desiredState: off\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runPreview(policy); err != nil {
		t.Fatalf("documented YAML input was rejected: %v (content-type=%s body=%q)", err, contentType, body)
	}
}

func TestAcceptanceRequestBodySurvivesTokenRefresh(t *testing.T) {
	isolatedCLI(t)
	var attempts atomic.Int32
	var retriedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accessToken":"new-access","refreshToken":"new-refresh"}`))
		case "/write":
			body, _ := io.ReadAll(r.Body)
			if attempts.Add(1) == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			retriedBody = string(body)
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "expired", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}

	payload := `{"spec":{"state":"off","reason":"acceptance"}}`
	response, err := authenticatedRequest(http.MethodPost, server.URL+"/write", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("retry status=%d", response.StatusCode)
	}
	if retriedBody != payload {
		t.Fatalf("request body changed after refresh: got %q want %q", retriedBody, payload)
	}
}
