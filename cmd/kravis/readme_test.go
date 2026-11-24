package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readme returns the text of README.md.
func readme(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoDir, "README.md"))
	must(t, err)
	return string(raw)
}

// quickstartCommands returns the kravis commands of the quickstart's code
// block, each split into arguments the way a shell would.
func quickstartCommands(t *testing.T, text string) [][]string {
	t.Helper()
	_, after, ok := strings.Cut(text, "## Five-minute quickstart")
	if !ok {
		t.Fatal("README has no quickstart")
	}
	_, block, ok := strings.Cut(after, "```sh\n")
	if !ok {
		t.Fatal("the quickstart has no code block")
	}
	block, _, _ = strings.Cut(block, "```")
	var cmds [][]string
	for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
		args := splitArgs(line)
		if len(args) < 2 || args[0] != "kravis" {
			t.Fatalf("not a kravis command: %q", line)
		}
		cmds = append(cmds, args[1:])
	}
	return cmds
}

// splitArgs splits a line on spaces, keeping double-quoted text together.
func splitArgs(line string) []string {
	var args []string
	var cur strings.Builder
	quoted, started := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted, started = !quoted, true
		case r == ' ' && !quoted:
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		args = append(args, cur.String())
	}
	return args
}

func TestReadmeDoesNotDrift(t *testing.T) {
	text := readme(t)
	if problems := skillProblems(text, treeOf(t)); len(problems) > 0 {
		t.Errorf("README cites what the binary lacks:\n%s", strings.Join(problems, "\n"))
	}
	if n := len(citedCommands(text)); n < 6 {
		t.Errorf("only %d commands cited: the extraction is broken", n)
	}
}

// TestReadmeQuickstartRuns runs the quickstart as written, in an empty
// workspace, and checks that the follow-up it creates is listed.
func TestReadmeQuickstartRuns(t *testing.T) {
	e := newEnv(t)
	cmds := quickstartCommands(t, readme(t))
	if len(cmds) != 6 {
		t.Fatalf("the quickstart has %d commands, want 6", len(cmds))
	}
	var last result
	for _, args := range cmds {
		last = e.run(args...)
		if last.Code != 0 {
			t.Fatalf("kravis %s: code %d, stderr %s", strings.Join(args, " "), last.Code, last.Stderr)
		}
	}
	tasks, _ := last.JSON(t)["data"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["title"] != "Send the proposal" {
		t.Errorf("task list = %s", last.Stdout)
	}
}
