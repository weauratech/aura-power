package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestChangePasswordCommandSendsExactContractAndClearsSession(t *testing.T) {
	setupCLIUnitTest(t)
	const current = "Current-password-17!"
	const next = "New-passphrase-28!"
	var gotAuthorization string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/auth/password" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"updated":true}`)
	}))
	defer server.Close()
	if err := saveConfig(&CLIConfig{ServerURL: server.URL, AccessToken: "access", RefreshToken: "refresh", Username: "member"}); err != nil {
		t.Fatal(err)
	}

	values := []string{current, next, next}
	originalReader := readPasswordSecret
	readPasswordSecret = func(string) (string, error) {
		value := values[0]
		values = values[1:]
		return value, nil
	}
	t.Cleanup(func() { readPasswordSecret = originalReader })
	cmd := NewRootCmd()
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"change-password"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotAuthorization != "Bearer access" {
		t.Fatalf("authorization=%q", gotAuthorization)
	}
	wantBody := map[string]string{"currentPassword": current, "newPassword": next}
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("body=%v want=%v", gotBody, wantBody)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != server.URL || cfg.AccessToken != "" || cfg.RefreshToken != "" || cfg.Username != "" {
		t.Fatalf("stale credentials remain: %+v", cfg)
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, current) || strings.Contains(combined, next) {
		t.Fatal("command output exposed a password")
	}
	if !strings.Contains(stdout.String(), "sign in again") {
		t.Fatalf("missing reauthentication guidance: %q", stdout.String())
	}
	changeCommand, _, err := cmd.Find([]string{"change-password"})
	if err != nil || changeCommand.Flags().Lookup("password") != nil {
		t.Fatal("change-password must not expose a password flag")
	}
}

func TestChangePasswordRejectsMismatchWithoutRequest(t *testing.T) {
	setupCLIUnitTest(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	want := &CLIConfig{ServerURL: server.URL, AccessToken: "access", RefreshToken: "refresh", Username: "member"}
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}
	err := runChangePassword("Current-password-17!", "New-passphrase-28!", "Different-passphrase-29!")
	if err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("err=%v", err)
	}
	if requests != 0 {
		t.Fatalf("requests=%d", requests)
	}
	got, _ := loadConfig()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config changed on validation failure: %+v", got)
	}
}

func TestChangePasswordPreservesSessionOnServerErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			setupCLIUnitTest(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"password rejected"}`)
			}))
			defer server.Close()
			want := &CLIConfig{ServerURL: server.URL, AccessToken: "access", RefreshToken: "refresh", Username: "member"}
			if err := saveConfig(want); err != nil {
				t.Fatal(err)
			}
			err := runChangePassword("Current-password-17!", "New-passphrase-28!", "New-passphrase-28!")
			if err == nil || !strings.Contains(err.Error(), http.StatusText(status)) || !strings.Contains(err.Error(), "password rejected") {
				t.Fatalf("err=%v", err)
			}
			got, _ := loadConfig()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("config changed after status %d: %+v", status, got)
			}
		})
	}
}
