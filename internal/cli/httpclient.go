package cli

import (
	"encoding/json"
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

var httpClient = &http.Client{Timeout: 30 * time.Second}

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
	token, err := getAccessToken()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	// If 401, try refresh
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if refreshErr := refreshTokenFlow(); refreshErr != nil {
			return nil, fmt.Errorf("session expired. Run 'aura-power login' again")
		}
		// Retry with new token
		token, _ = getAccessToken()
		req, _ = http.NewRequest(method, url, body)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return httpClient.Do(req)
	}

	return resp, nil
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

	payload := fmt.Sprintf(`{"refreshToken":"%s"}`, cfg.RefreshToken)
	resp, err := http.Post(serverURL+"/api/v1/auth/refresh", "application/json", strings.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh failed with status %d", resp.StatusCode)
	}

	var tokens struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return err
	}

	cfg.AccessToken = tokens.AccessToken
	cfg.RefreshToken = tokens.RefreshToken
	return saveConfig(cfg)
}
