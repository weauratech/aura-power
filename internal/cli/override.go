package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newOverrideCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "override",
		Short: "Manage power overrides",
	}
	cmd.AddCommand(newOverrideCreateCmd())
	return cmd
}

func newOverrideCreateCmd() *cobra.Command {
	var (
		target   string
		state    string
		duration string
		reason   string
		ref      string
		priority int32
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a temporary power override",
		Long:  "Create a temporary override to power on/off a workload with mandatory expiration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverrideCreate(target, state, duration, reason, ref, priority)
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "Target workload (namespace/name or namespace)")
	cmd.Flags().StringVar(&state, "state", "", "Desired state: on or off")
	cmd.Flags().StringVar(&duration, "duration", "", "Duration (e.g., 3h, 30m, 1h30m)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for the override")
	cmd.Flags().StringVar(&ref, "reference", "", "External reference (ticket, incident)")
	cmd.Flags().Int32Var(&priority, "priority", 100, "Override priority (default: 100)")

	cmd.MarkFlagRequired("target")
	cmd.MarkFlagRequired("state")
	cmd.MarkFlagRequired("duration")
	cmd.MarkFlagRequired("reason")

	return cmd
}

func runOverrideCreate(target, state, durationStr, reason, ref string, priority int32) error {
	serverURL, err := getServerURL()
	if err != nil {
		return err
	}

	// Parse target
	parts := strings.SplitN(target, "/", 2)
	var namespaces []string
	var workloadNames []string
	if len(parts) == 2 {
		namespaces = []string{parts[0]}
		workloadNames = []string{parts[1]}
	} else {
		namespaces = []string{parts[0]}
	}

	// Parse duration
	dur, err := time.ParseDuration(durationStr)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", durationStr, err)
	}

	// Validate state
	if state != "on" && state != "off" {
		return fmt.Errorf("state must be 'on' or 'off', got %q", state)
	}

	expiresAt := time.Now().Add(dur).Format(time.RFC3339)

	// Build JSON payload
	payload := map[string]interface{}{
		"metadata": map[string]interface{}{
			"generateName": "override-",
			"namespace":    "aura-system",
		},
		"spec": map[string]interface{}{
			"scope": map[string]interface{}{
				"namespaces":    namespaces,
				"workloadNames": workloadNames,
			},
			"state":     state,
			"priority":  priority,
			"expiresAt": expiresAt,
			"reason":    reason,
			"reference": ref,
		},
	}

	data, _ := json.Marshal(payload)

	resp, err := authenticatedRequest("POST", serverURL+"/api/v1/overrides", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 201 {
		return fmt.Errorf("failed to create override (%d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	fmt.Printf("Override created: %v\n", result["name"])
	fmt.Printf("  Target:    %s\n", target)
	fmt.Printf("  State:     %s\n", state)
	fmt.Printf("  Expires:   %s\n", expiresAt)
	fmt.Printf("  Reason:    %s\n", reason)
	if ref != "" {
		fmt.Printf("  Reference: %s\n", ref)
	}

	return nil
}
