package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestVersionProcessUsesBuildMetadata(t *testing.T) {
	command := helperProcess(t, "version", "-o", "json")
	command.Env = append(command.Env, "ADMITRACE_HELPER_BUILD_METADATA=1")

	output, err := command.Output()
	if err != nil {
		t.Fatalf("admitrace version error = %v", err)
	}
	for _, want := range []string{
		`"version": "v0.1.0-process-test"`,
		`"commit": "process-test-commit"`,
		`"buildDate": "2026-07-13T01:02:03Z"`,
	} {
		if got := string(output); !strings.Contains(got, want) {
			t.Errorf("admitrace version output = %q, want substring %q", got, want)
		}
	}
}

func TestInvalidOutputProcessExit(t *testing.T) {
	command := helperProcess(t, "version", "-o", "yaml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("admitrace version error = %v, want process exit error", err)
	}
	if got := exitErr.ExitCode(); got != 2 {
		t.Errorf("admitrace version exit code = %d, want 2", got)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("admitrace version stdout = %q, want empty", got)
	}
	for _, want := range []string{"invalid output format", "Usage:"} {
		if got := stderr.String(); !strings.Contains(got, want) {
			t.Errorf("admitrace version stderr = %q, want substring %q", got, want)
		}
	}
}

func TestAdmitraceHelperProcess(t *testing.T) {
	if os.Getenv("ADMITRACE_HELPER_PROCESS") != "1" {
		return
	}

	separator := 0
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator == 0 {
		os.Exit(4)
	}

	if os.Getenv("ADMITRACE_HELPER_BUILD_METADATA") == "1" {
		version = "v0.1.0-process-test"
		commit = "process-test-commit"
		buildDate = "2026-07-13T01:02:03Z"
	}
	os.Args = append([]string{"admitrace"}, os.Args[separator+1:]...)
	main()
}

func helperProcess(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()

	commandArgs := []string{"-test.run=TestAdmitraceHelperProcess", "--"}
	commandArgs = append(commandArgs, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), "ADMITRACE_HELPER_PROCESS=1")
	return command
}
