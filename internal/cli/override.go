package cli

import (
	"encoding/json"
	"fmt"
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
	var targetRefs []overrideTargetReference
	if len(parts) == 2 {
		if kind == "" {
			return fmt.Errorf("kind is required when target identifies a workload")
		}
		if !validWorkloadKind(kind) {
			return fmt.Errorf("invalid workload kind %q", kind)
		}
		targetRefs = []overrideTargetReference{{Namespace: parts[0], Name: parts[1], Kind: kind, UID: uid}}
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
	payload := overrideCreateRequest{}
	payload.Metadata.GenerateName = "override-"
	payload.Spec.Scope.Namespaces = namespaces
	payload.Spec.Scope.TargetRefs = targetRefs
	payload.Spec.State = state
	payload.Spec.Priority = priority
	payload.Spec.ExpiresAt = expiresAt
	payload.Spec.Reason = reason
	payload.Spec.Reference = ref

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode override: %w", err)
	}

	resp, err := authenticatedRequestBytes("POST", serverURL+"/api/v1/overrides", data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := requireStatus(resp, 201)
	if err != nil {
		return fmt.Errorf("failed to create override: %w", err)
	}

	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode override response: %w", err)
	}

	fmt.Printf("Override created: %s\n", result.Name)
	fmt.Printf("  Target:    %s\n", target)
	fmt.Printf("  State:     %s\n", state)
	fmt.Printf("  Expires:   %s\n", expiresAt)
	fmt.Printf("  Reason:    %s\n", reason)
	if ref != "" {
		fmt.Printf("  Reference: %s\n", ref)
	}

	return nil
}

type overrideTargetReference struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	UID       string `json:"uid,omitempty"`
}

type overrideCreateRequest struct {
	Metadata struct {
		GenerateName string `json:"generateName"`
		Namespace    string `json:"namespace,omitempty"`
	} `json:"metadata"`
	Spec struct {
		Scope struct {
			Namespaces []string                  `json:"namespaces,omitempty"`
			TargetRefs []overrideTargetReference `json:"targetRefs,omitempty"`
		} `json:"scope"`
		State     string `json:"state"`
		Priority  int32  `json:"priority"`
		ExpiresAt string `json:"expiresAt"`
		Reason    string `json:"reason"`
		Reference string `json:"reference,omitempty"`
	} `json:"spec"`
}

func validWorkloadKind(kind string) bool {
	return kind == "Deployment" || kind == "StatefulSet" || kind == "CronJob"
}
