package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

	payload, err := policyPreviewJSON(data)
	if err != nil {
		return fmt.Errorf("invalid policy file %s: %w", file, err)
	}

	resp, err := authenticatedRequestBytes("POST", serverURL+"/api/v1/preview/policy", payload)
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

	fmt.Println("\nPolicy Impact Preview")
	fmt.Println("─────────────────────")
	for key, val := range result {
		fmt.Printf("  %s: %v\n", key, val)
	}
	fmt.Println()

	return nil
}

// policyPreviewJSON accepts the documented YAML (or JSON) representation. A
// full PowerPolicy resource is reduced to its spec because the preview endpoint
// consumes PowerPolicySpec; a spec-only document is passed through unchanged.
func policyPreviewJSON(data []byte) ([]byte, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if len(document) == 0 {
		return nil, fmt.Errorf("policy document is empty")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple YAML documents are not supported")
		}
		return nil, err
	}
	if spec, ok := document["spec"]; ok {
		if _, ok := spec.(map[string]any); !ok {
			return nil, fmt.Errorf("spec must be an object")
		}
		return json.Marshal(spec)
	}
	return json.Marshal(document)
}
