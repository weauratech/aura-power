package cli

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newSavingsCmd() *cobra.Command {
	var period string

	cmd := &cobra.Command{
		Use:   "savings",
		Short: "Display savings summary",
		Long:  "Show accumulated compute-hours and estimated cost savings.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavings(period)
		},
	}

	cmd.Flags().StringVar(&period, "period", "30d", "Time period: 7d, 30d, 90d")
	return cmd
}

func runSavings(period string) error {
	serverURL, err := getServerURL()
	if err != nil {
		return err
	}

	endpoint, err := url.Parse(serverURL + "/api/v1/savings")
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("period", period)
	if namespace != "" {
		query.Set("namespace", namespace)
	}
	endpoint.RawQuery = query.Encode()

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

	fmt.Println("\nSavings Summary")
	fmt.Println("───────────────")
	if v, ok := result["totalCPUHours"]; ok {
		fmt.Printf("  CPU Hours Saved:     %.1f\n", v.(float64))
	}
	if v, ok := result["totalMemoryGiB"]; ok {
		fmt.Printf("  Memory GiB-h Saved:  %.1f\n", v.(float64))
	}
	if v, ok := result["totalEstimatedCost"]; ok {
		fmt.Printf("  Estimated Savings:   $%.2f\n", v.(float64))
	}
	fmt.Println()

	return nil
}
