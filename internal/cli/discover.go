package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Display discovery mode summary",
		Long:  "Show discovered workloads, eligibility, and estimated savings potential.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscover()
		},
	}
	return cmd
}

func runDiscover() error {
	serverURL, err := getServerURL()
	if err != nil {
		return err
	}

	endpoint := serverURL + "/api/v1/discover"
	if namespace != "" {
		endpoint += "?namespace=" + namespace
	}

	resp, err := authenticatedGet(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
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

	fmt.Println("\nDiscovery Mode Summary")
	fmt.Println("──────────────────────")
	if v, ok := result["totalWorkloads"]; ok {
		fmt.Printf("  Total Workloads:  %.0f\n", v.(float64))
	}
	if v, ok := result["eligible"]; ok {
		fmt.Printf("  Eligible:         %.0f\n", v.(float64))
	}
	if v, ok := result["blocked"]; ok {
		fmt.Printf("  Blocked:          %.0f\n", v.(float64))
	}
	fmt.Println()

	return nil
}
