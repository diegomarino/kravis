package main

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes, as in docs/cli.md.
const (
	exitOK    = 0
	exitUsage = 2
)

// run executes the CLI in process and returns its exit code. Every test
// calls it; main only forwards the process arguments and streams.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRootCommand()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		writeUsageError(stderr, err)
		return exitUsage
	}
	return exitOK
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "kravis",
		Short:         "A CRM for agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	root.SetVersionTemplate("") // the version is printed as JSON below
	root.Flags().Bool("version", false, "print the version")
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if v, _ := cmd.Flags().GetBool("version"); v {
			return writeJSON(cmd.OutOrStdout(), map[string]any{"data": map[string]any{"version": version}})
		}
		return cmd.Help()
	}
	return root
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	return enc.Encode(v)
}

// writeUsageError prints the error envelope of docs/cli.md on stderr.
func writeUsageError(w io.Writer, err error) {
	code := "INVALID_VALUE"
	if strings.HasPrefix(err.Error(), "unknown flag") || strings.HasPrefix(err.Error(), "unknown shorthand flag") {
		code = "UNKNOWN_FIELD"
	}
	_ = writeJSON(w, map[string]any{"error": map[string]any{
		"code":      code,
		"message":   err.Error(),
		"retryable": false,
		"hint":      "run kravis --help",
	}})
}
