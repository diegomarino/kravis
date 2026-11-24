package specgate

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner runs a command and returns its standard output.
type Runner func(name string, args ...string) ([]byte, error)

// ExecRunner runs real commands, in the current folder.
func ExecRunner(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Exit codes of Main.
const (
	ExitAdditive    = 0
	ExitNeedsPerson = 1
	ExitFailure     = 2
)

var guardedDocs = []string{"GLOSSARY.md", "docs/cli.md", "docs/storage.md"}

// Main runs the gate: kravis's own spec rules between --base and HEAD.
func Main(args []string, stdout, stderr io.Writer, run Runner) int {
	fs := flag.NewFlagSet("specgate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	base := fs.String("base", "", "the base git revision, for example origin/main")
	approved := fs.Bool("approved", false, "the pull request carries the spec-approved label")
	if err := fs.Parse(args); err != nil {
		return ExitFailure
	}
	if *base == "" {
		say(stderr, "specgate: --base is required")
		return ExitFailure
	}
	reasons, err := gather(*base, run)
	if err != nil {
		say(stderr, "specgate:", err)
		return ExitFailure
	}
	if len(reasons) == 0 {
		say(stdout, "spec gate: additive change")
		return ExitAdditive
	}
	say(stdout, "spec gate: this change needs the person:")
	for _, r := range reasons {
		say(stdout, " -", r)
	}
	if *approved {
		say(stdout, "spec gate: approved by the spec-approved label")
		return ExitAdditive
	}
	say(stdout, "spec gate: add the spec-approved label to let it through")
	return ExitNeedsPerson
}

func gather(base string, run Runner) ([]string, error) {
	var reasons []string

	out, err := run("git", "diff", "--name-status", base+"...HEAD", "--", "docs/ddd/decisions")
	if err != nil {
		return nil, err
	}
	reasons = append(reasons, ClassifyNameStatus(string(out))...)

	args := append([]string{"diff", "-U0", base + "...HEAD", "--"}, guardedDocs...)
	if out, err = run("git", args...); err != nil {
		return nil, err
	}
	reasons = append(reasons, ClassifyLineDiff(string(out))...)

	if out, err = run("git", "log", "--format=%s", base+"..HEAD"); err != nil {
		return nil, err
	}
	reasons = append(reasons, ClassifySubjects(strings.Split(string(out), "\n"))...)

	model, err := modelReasons(base, run)
	if err != nil {
		return nil, err
	}
	return append(reasons, model...), nil
}

// modelReasons compares docs/ddd at the base with the working tree. A base
// without a model has nothing to lose, so everything is additive.
func modelReasons(base string, run Runner) ([]string, error) {
	if out, err := run("git", "ls-tree", "--name-only", base, "docs/ddd"); err != nil {
		return nil, err
	} else if strings.TrimSpace(string(out)) == "" {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "specgate-base-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tarball := filepath.Join(dir, "base.tar")
	if _, err := run("git", "archive", "--output", tarball, base, "docs/ddd"); err != nil {
		return nil, err
	}
	if _, err := run("tar", "-xf", tarball, "-C", dir); err != nil {
		return nil, err
	}
	doc, err := run("ddduck", "diff", "--base", filepath.Join(dir, "docs", "ddd"), "--root", "docs/ddd", "--json")
	if err != nil {
		return nil, err
	}
	return ClassifyModelDiff(doc)
}

// say writes a line; a failed write of a report has nowhere else to go.
func say(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }
