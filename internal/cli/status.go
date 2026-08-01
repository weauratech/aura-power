package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var stateFilter string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display target power state summary",
		Long:  "Show all managed workloads grouped by namespace with their current power state.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(stateFilter)
		},
	}

	cmd.Flags().StringVar(&stateFilter, "state", "", "Filter by state: on, off, blocked, divergent")
	return cmd
}

func runStatus(stateFilter string) error {
	serverURL, err := getServerURL()
	if err != nil {
		return err
	}

	endpoint := serverURL + "/api/v1/targets"
	sep := "?"
	if namespace != "" {
		endpoint += sep + "namespace=" + namespace
		sep = "&"
	}
	if stateFilter != "" {
		endpoint += sep + "state=" + stateFilter
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

	var result struct {
		Targets []struct {
			Spec struct {
				TargetRef struct {
					Namespace string `json:"namespace"`
					Name      string `json:"name"`
					Kind      string `json:"kind"`
				} `json:"targetRef"`
			} `json:"spec"`
			Status struct {
				DesiredState string `json:"desiredState"`
				ObservedState struct {
					PowerState string `json:"powerState"`
				} `json:"observedState"`
				Blocked   bool `json:"blocked"`
				Divergent bool `json:"divergent"`
			} `json:"status"`
		} `json:"targets"`
		Count int `json:"count"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Println(string(body))
		return nil
	}

	if len(result.Targets) == 0 {
		fmt.Println("No targets found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "NAMESPACE\tNAME\tKIND\tSTATE\tOBSERVED\tBLOCKED\tDIVERGENT\n")

	for _, t := range result.Targets {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\t%v\n",
			t.Spec.TargetRef.Namespace,
			t.Spec.TargetRef.Name,
			t.Spec.TargetRef.Kind,
			t.Status.DesiredState,
			t.Status.ObservedState.PowerState,
			t.Status.Blocked,
			t.Status.Divergent,
		)
	}
	w.Flush()

	fmt.Printf("\nTotal: %d targets\n", result.Count)
	return nil
}

func outputJSON(data interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
