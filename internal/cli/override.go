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
		kind     string
		uid      string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a temporary power override",
		Long:  "Create a temporary override to power on/off a workload with mandatory expiration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverrideCreate(target, kind, uid, state, duration, reason, ref, priority)
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "Target workload (namespace/name or namespace)")
	cmd.Flags().StringVar(&state, "state", "", "Desired state: on or off")
	cmd.Flags().StringVar(&duration, "duration", "", "Duration (e.g., 3h, 30m, 1h30m)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for the override")
	cmd.Flags().StringVar(&ref, "reference", "", "External reference (ticket, incident)")
	cmd.Flags().Int32Var(&priority, "priority", 100, "Override priority (default: 100)")
	cmd.Flags().StringVar(&kind, "kind", "", "Workload kind when --target is namespace/name")
	cmd.Flags().StringVar(&uid, "uid", "", "Optional Kubernetes UID for an exact object incarnation")

	cmd.MarkFlagRequired("target")
	cmd.MarkFlagRequired("state")
	cmd.MarkFlagRequired("duration")
	cmd.MarkFlagRequired("reason")

	return cmd
}

func runOverrideCreate(target, kind, uid, state, durationStr, reason, ref string, priority int32) error {
	serverURL, err := getServerURL()
	if err != nil {
		return err
	}

	// Parse target
	parts := strings.SplitN(target, "/", 2)
	var namespaces []string
	var targetRefs []map[string]string
	if len(parts) == 2 {
		if kind == "" {
			return fmt.Errorf("kind is required when target identifies a workload")
		}
		if !validWorkloadKind(kind) {
			return fmt.Errorf("invalid workload kind %q", kind)
		}
		targetRef := map[string]string{"namespace": parts[0], "name": parts[1], "kind": kind}
		if uid != "" {
			targetRef["uid"] = uid
		}
		targetRefs = []map[string]string{targetRef}
	} else {
		namespaces = []string{parts[0]}
		if kind != "" || uid != "" {
			return fmt.Errorf("kind and uid require a namespace/name target")
		}
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
	scope := map[string]interface{}{}
	if len(namespaces) > 0 {
		scope["namespaces"] = namespaces
	}
	if len(targetRefs) > 0 {
		scope["targetRefs"] = targetRefs
	}

	// Build JSON payload
	payload := map[string]interface{}{
		"metadata": map[string]interface{}{
			"generateName": "override-",
			"namespace":    "aura-system",
		},
		"spec": map[string]interface{}{
			"scope":     scope,
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

func validWorkloadKind(kind string) bool {
	return kind == "Deployment" || kind == "StatefulSet" || kind == "CronJob"
}
