package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const passwordPolicySummary = "Use at least 12 characters and at most 72 UTF-8 bytes; avoid common or repetitive passwords."

var readPasswordSecret = func(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func newChangePasswordCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "change-password",
		Short: "Change your password and end the current CLI session",
		Long: "Change the password for the authenticated user. " + passwordPolicySummary +
			" Passwords are read from a hidden terminal prompt and are never accepted as command-line arguments or flags.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), passwordPolicySummary)
			currentPassword, err := readPasswordSecret("Current password: ")
			if err != nil {
				return fmt.Errorf("read current password: %w", err)
			}
			newPassword, err := readPasswordSecret("New password: ")
			if err != nil {
				return fmt.Errorf("read new password: %w", err)
			}
			confirmation, err := readPasswordSecret("Confirm new password: ")
			if err != nil {
				return fmt.Errorf("read password confirmation: %w", err)
			}
			if err := runChangePassword(currentPassword, newPassword, confirmation); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Password changed. Stored session credentials were removed; sign in again with 'aura-power login'.")
			return nil
		},
	}
}

func runChangePassword(currentPassword, newPassword, confirmation string) error {
	if currentPassword == "" {
		return fmt.Errorf("current password is required")
	}
	if newPassword != confirmation {
		return fmt.Errorf("new password and confirmation do not match")
	}
	if utf8.RuneCountInString(newPassword) < 12 || len([]byte(newPassword)) > 72 {
		return fmt.Errorf("new password does not satisfy the password policy: %s", passwordPolicySummary)
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	serverURL := strings.TrimRight(cfg.ServerURL, "/")
	if apiURL != "" {
		serverURL = strings.TrimRight(apiURL, "/")
	}
	if serverURL == "" {
		return fmt.Errorf("no server URL configured. Run 'aura-power login --server <URL>' first")
	}
	if cfg.AccessToken == "" {
		return fmt.Errorf("not logged in. Run 'aura-power login' first")
	}

	payload, err := json.Marshal(struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}{CurrentPassword: currentPassword, NewPassword: newPassword})
	if err != nil {
		return fmt.Errorf("encode password change: %w", err)
	}
	resp, err := doJSONRequest(http.MethodPut, serverURL+"/api/v1/auth/password", cfg.AccessToken, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := requireStatus(resp, http.StatusOK); err != nil {
		return fmt.Errorf("password change failed: %w", err)
	}

	// The server increments auth_version, invalidating every access and refresh
	// token. Remove the now-stale local credentials while retaining the endpoint.
	if err := saveConfig(&CLIConfig{ServerURL: serverURL}); err != nil {
		return fmt.Errorf("password changed, but failed to remove stale local credentials: %w", err)
	}
	return nil
}
