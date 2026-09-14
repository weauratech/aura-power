package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandReportsInjectedBuildIdentity(t *testing.T) {
	cmd := NewRootCmdWithVersion("2.2.0", "deadbeef")
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	if got := output.String(); !strings.Contains(got, "2.2.0 (commit deadbeef)") {
		t.Fatalf("version output %q does not contain injected build identity", got)
	}
}
