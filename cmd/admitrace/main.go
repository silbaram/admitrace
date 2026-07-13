package main

import (
	"os"

	"github.com/silbaram/admitrace/internal/cli"
)

var (
	version   = "devel"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	exitCode := cli.Execute(os.Args[1:], os.Stdout, os.Stderr, cli.BuildMetadata{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	})
	os.Exit(int(exitCode))
}
