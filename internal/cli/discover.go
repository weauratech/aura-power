package cli

import (
	"encoding/json"
	"fmt"
	"net/url"

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

	endpoint, err := url.Parse(serverURL + "/api/v1/discover")
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}
	if namespace != "" {
		query := endpoint.Query()
		query.Set("namespace", namespace)
		endpoint.RawQuery = query.Encode()
	}

	resp, err := authenticatedGet(endpoint.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := requireStatus(resp, 200)
	if err != nil {
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
