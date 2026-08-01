package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newPreviewCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Preview policy impact before applying",
		Long:  "Compute and display the impact preview for a policy YAML before applying.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPreview(file)
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to policy YAML file")
	cmd.MarkFlagRequired("file")

	return cmd
}

func runPreview(file string) error {
	// Read policy file
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", file, err)
	}

	serverURL, err := getServerURL()
	if err != nil {
		return err
	}

	resp, err := authenticatedRequest("POST", serverURL+"/api/v1/preview/policy", strings.NewReader(string(data)))
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

	fmt.Println("\nPolicy Impact Preview")
	fmt.Println("─────────────────────")
	for key, val := range result {
		fmt.Printf("  %s: %v\n", key, val)
	}
	fmt.Println()

	return nil
}
