package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var (
	outputFormat string
	namespace    string
	apiURL       string
)

// NewRootCmd creates the root command for the aura-power CLI.
func NewRootCmd() *cobra.Command {
	return NewRootCmdWithVersion("dev", "unknown")
}

// NewRootCmdWithVersion creates the root command with build identity exposed by
// Cobra's standard --version flag.
func NewRootCmdWithVersion(version, commit string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "aura-power",
		Short:   "Aura Power — Kubernetes workload energy governance",
		Version: fmt.Sprintf("%s (commit %s)", version, commit),
		Long: `Aura Power CLI provides terminal-based operations for managing
workload power policies, overrides, and monitoring savings.

Control when your Kubernetes workloads need to be on — and safely
shut them down when they don't.

Use 'aura-power login --server <URL>' to authenticate with the server.`,
	}

	// Global flags
	cmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "", "Output format: json, yaml (default: human-readable)")
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Filter by namespace")
	cmd.PersistentFlags().StringVar(&apiURL, "server-url", "", "Server URL (overrides stored config)")
	cmd.PersistentFlags().DurationVar(&requestTimeout, "request-timeout", 30*time.Second, "Timeout for each server request")

	// Auth commands
	cmd.AddCommand(newLoginCmd())
	cmd.AddCommand(newLogoutCmd())
	cmd.AddCommand(newWhoamiCmd())
	cmd.AddCommand(newChangePasswordCmd())

	// Data commands
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newExplainCmd())
	cmd.AddCommand(newPreviewCmd())
	cmd.AddCommand(newOverrideCmd())
	cmd.AddCommand(newSavingsCmd())
	cmd.AddCommand(newDiscoverCmd())

	return cmd
}
