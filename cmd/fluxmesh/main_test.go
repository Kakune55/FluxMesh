package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestMainExitsOnInvalidConfig(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
		flag.CommandLine.SetOutput(io.Discard)
		os.Args = []string{"fluxmesh"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMainExitsOnInvalidConfig")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"FLUXMESH_ROLE=agent",
		"FLUXMESH_NODE_ID=",
		"FLUXMESH_SEED_ENDPOINTS=http://127.0.0.1:2379",
	)

	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected process exit error, got %v", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got %d", exitErr.ExitCode())
	}
}
