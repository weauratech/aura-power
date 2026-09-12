package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CLIConfig holds persistent CLI configuration (stored in ~/.aura/config.yaml).
type CLIConfig struct {
	ServerURL    string `yaml:"server_url"`
	AccessToken  string `yaml:"access_token"`
	RefreshToken string `yaml:"refresh_token"`
	Username     string `yaml:"username"`
}

var httpClient = &http.Client{}
var requestTimeout = 30 * time.Second

const maxErrorBodyBytes = 64 << 10

// HTTPStatusError preserves the server status and response body for callers and
// scripts while still providing a useful command-line error.
type HTTPStatusError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("server returned %s", e.Status)
	}
	return fmt.Sprintf("server returned %s: %s", e.Status, e.Body)
}

// configPath returns the path to the CLI config file.
func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aura", "config.yaml")
}

// loadConfig loads the CLI config from disk.
func loadConfig() (*CLIConfig, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &CLIConfig{}, nil
		}
		return nil, err
	}
	var cfg CLIConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// saveConfig writes the CLI config to disk.
func saveConfig(cfg *CLIConfig) error {
	dir := filepath.Dir(configPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

// getServerURL returns the server URL from flag or config.
func getServerURL() (string, error) {
	// Flag takes priority
	if apiURL != "" {
		return strings.TrimRight(apiURL, "/"), nil
	}
	// Load from config
	cfg, err := loadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.ServerURL != "" {
		return strings.TrimRight(cfg.ServerURL, "/"), nil
	}
	return "", fmt.Errorf("no server URL configured. Run 'aura-power login --server <URL>' first")
}

// getAccessToken returns the stored access token.
func getAccessToken() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if cfg.AccessToken == "" {
		return "", fmt.Errorf("not logged in. Run 'aura-power login' first")
	}
	return cfg.AccessToken, nil
}

// authenticatedRequest makes an HTTP request with JWT token.
// If 401 is returned, it attempts a token refresh automatically.
func authenticatedRequest(method, url string, body io.Reader) (*http.Response, error) {
	var payload []byte
	var err error
	if body != nil {
		payload, err = io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
	}
	return authenticatedRequestBytes(method, url, payload)
}

func authenticatedRequestBytes(method, url string, payload []byte) (*http.Response, error) {
	token, err := getAccessToken()
	if err != nil {
		return nil, err
	}

	resp, err := doJSONRequest(method, url, token, payload)
	if err != nil {
		return nil, err
	}

	// If 401, try refresh
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if refreshErr := refreshTokenFlow(); refreshErr != nil {
			return nil, fmt.Errorf("session expired; run 'aura-power login' again: %w", refreshErr)
		}
		// Retry with new token
		token, err = getAccessToken()
		if err != nil {
			return nil, err
		}
		return doJSONRequest(method, url, token, payload)
	}

	return resp, nil
}

func doJSONRequest(method, url, token string, payload []byte) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	ctx := context.Background()
	cancel := func() {}
	if requestTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		cancel()
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
			return nil, fmt.Errorf("request timed out after %s: %w", requestTimeout, err)
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	resp.Body = &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

// authenticatedGet is a convenience for GET requests.
func authenticatedGet(url string) (*http.Response, error) {
	return authenticatedRequest("GET", url, nil)
}

// refreshTokenFlow attempts to refresh the JWT using the stored refresh token.
func refreshTokenFlow() error {
	cfg, err := loadConfig()
	if err != nil || cfg.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	serverURL := cfg.ServerURL
	if apiURL != "" {
		serverURL = strings.TrimRight(apiURL, "/")
	}

	payload, err := json.Marshal(struct {
		RefreshToken string `json:"refreshToken"`
	}{RefreshToken: cfg.RefreshToken})
	if err != nil {
		return fmt.Errorf("encode refresh request: %w", err)
	}
	resp, err := doJSONRequest(http.MethodPost, serverURL+"/api/v1/auth/refresh", "", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := requireStatus(resp, http.StatusOK)
	if err != nil {
		return fmt.Errorf("refresh failed: %w", err)
	}

	var tokens struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.Unmarshal(body, &tokens); err != nil {
		return err
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return fmt.Errorf("refresh response did not contain both tokens")
	}

	cfg.AccessToken = tokens.AccessToken
	cfg.RefreshToken = tokens.RefreshToken
	return saveConfig(cfg)
}

func requireStatus(resp *http.Response, expected ...int) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	for _, status := range expected {
		if resp.StatusCode == status {
			return body, nil
		}
	}
	message := strings.TrimSpace(string(body))
	if len(message) > maxErrorBodyBytes {
		message = message[:maxErrorBodyBytes] + "..."
	}
	return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Status: resp.Status, Body: message}
}
