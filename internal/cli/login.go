package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"os"
)

func newLoginCmd() *cobra.Command {
	var server, username, password string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the Aura Power server",
		Long:  "Login to the Aura Power server using username and password. Stores JWT tokens locally.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(server, username, password)
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Server URL (e.g., https://power.aura.sh)")
	cmd.Flags().StringVar(&username, "username", "", "Username")
	cmd.Flags().StringVar(&password, "password", "", "Password (will prompt if not provided)")

	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout()
		},
	}
}

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current authenticated user",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhoami()
		},
	}
}

func runLogin(server, username, password string) error {
	// Resolve server URL
	if server == "" {
		cfg, _ := loadConfig()
		if cfg != nil && cfg.ServerURL != "" {
			server = cfg.ServerURL
		} else if apiURL != "" {
			server = apiURL
		} else {
			return fmt.Errorf("--server is required on first login")
		}
	}
	server = strings.TrimRight(server, "/")

	// Prompt for username if not provided
	if username == "" {
		fmt.Print("Username: ")
		fmt.Scanln(&username)
	}

	// Prompt for password if not provided
	if password == "" {
		fmt.Print("Password: ")
		pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		password = string(pwBytes)
		fmt.Println() // newline after hidden input
	}

	// Call login API
	payload := fmt.Sprintf(`{"username":"%s","password":"%s"}`, username, password)
	resp, err := http.Post(server+"/api/v1/auth/login", "application/json", strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid credentials")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed with status %d", resp.StatusCode)
	}

	var tokens struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Save config
	cfg := &CLIConfig{
		ServerURL:    server,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		Username:     username,
	}
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Printf("Logged in as %s to %s\n", username, server)
	return nil
}

func runLogout() error {
	cfg := &CLIConfig{}
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("failed to clear credentials: %w", err)
	}
	fmt.Println("Logged out")
	return nil
}

func runWhoami() error {
	serverURL, err := getServerURL()
	if err != nil {
		return err
	}

	resp, err := authenticatedGet(serverURL + "/api/v1/auth/me")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get user info (status %d)", resp.StatusCode)
	}

	var user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return err
	}

	fmt.Printf("Username: %s\n", user.Username)
	fmt.Printf("Role:     %s\n", user.Role)
	fmt.Printf("Server:   %s\n", serverURL)
	return nil
}
