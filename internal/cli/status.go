package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
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

	endpoint, err := url.Parse(serverURL + "/api/v1/targets")
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}
	query := endpoint.Query()
	if namespace != "" {
		query.Set("namespace", namespace)
	}
	if stateFilter != "" {
		query.Set("state", stateFilter)
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
				DesiredState  string `json:"desiredState"`
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
