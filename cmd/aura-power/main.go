package main

import (
	"fmt"
	"os"

	"github.com/weauratech/aura-power/internal/cli"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	rootCmd := cli.NewRootCmdWithVersion(version, commit)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
