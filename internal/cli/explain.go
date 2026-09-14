package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newExplainCmd() *cobra.Command {
	var kind, uid string
	cmd := &cobra.Command{
		Use:   "explain <namespace>/<workload>",
		Short: "Explain the current state of a workload",
		Long:  "Display full explainability for a workload: desired state, winning rule, blocks, snapshot, ownership.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExplain(args[0], kind, uid)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "Workload kind (Deployment, StatefulSet, or CronJob)")
	cmd.Flags().StringVar(&uid, "uid", "", "Optional Kubernetes UID for an exact object incarnation")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func runExplain(target, kind, uid string) error {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("target must be in format <namespace>/<name>")
	}
	ns, name := parts[0], parts[1]
	if kind == "" {
		return fmt.Errorf("kind is required to identify a workload exactly")
	}
	if !validWorkloadKind(kind) {
		return fmt.Errorf("invalid workload kind %q", kind)
	}

	serverURL, err := getServerURL()
	if err != nil {
		return err
	}

	query := url.Values{"kind": []string{kind}}
	if uid != "" {
		query.Set("uid", uid)
	}
	endpoint := fmt.Sprintf("%s/api/v1/targets/%s/%s/explain?%s", serverURL, url.PathEscape(ns), url.PathEscape(name), query.Encode())
	resp, err := authenticatedGet(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := requireStatus(resp, 200)
	if err != nil {
		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("target %s/%s not found: %w", ns, name, err)
		}
		return err
	}

	if outputFormat == "json" {
		fmt.Println(string(body))
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Println(string(body))
		return nil
	}

	fmt.Printf("\n%s/%s\n", ns, name)
	fmt.Println(strings.Repeat("─", 50))

	if v, ok := result["effectiveState"]; ok {
		fmt.Printf("  Effective State:  %v\n", v)
	}
	if v, ok := result["observedState"]; ok {
		fmt.Printf("  Observed State:   %v\n", v)
	}
	if v, ok := result["blocked"]; ok {
		fmt.Printf("  Blocked:          %v\n", v)
	}

	if wr, ok := result["winningRule"]; ok && wr != nil {
		fmt.Printf("\n  Winning Rule:\n")
		if wrMap, ok := wr.(map[string]interface{}); ok {
			if v, ok := wrMap["kind"]; ok {
				fmt.Printf("    Kind:        %v\n", v)
			}
			if v, ok := wrMap["name"]; ok {
				fmt.Printf("    Name:        %v\n", v)
			}
			if v, ok := wrMap["priority"]; ok {
				fmt.Printf("    Priority:    %v\n", v)
			}
		}
	}

	if reasons, ok := result["blockReasons"]; ok && reasons != nil {
		if arr, ok := reasons.([]interface{}); ok && len(arr) > 0 {
			fmt.Printf("\n  Block Reasons:\n")
			for _, r := range arr {
				if m, ok := r.(map[string]interface{}); ok {
					fmt.Printf("    - [%v] %v\n", m["type"], m["message"])
				}
			}
		}
	}

	if savings, ok := result["savings"]; ok && savings != nil {
		if m, ok := savings.(map[string]interface{}); ok {
			fmt.Printf("\n  Savings:\n")
			if v, ok := m["cpuHoursSaved"]; ok {
				fmt.Printf("    CPU Hours:     %.1f\n", v.(float64))
			}
			if v, ok := m["memoryGiBHours"]; ok {
				fmt.Printf("    Memory GiB-h:  %.1f\n", v.(float64))
			}
			if v, ok := m["estimatedCost"]; ok {
				fmt.Printf("    Est. Cost:     $%.2f\n", v.(float64))
			}
		}
	}

	fmt.Println()
	return nil
}
