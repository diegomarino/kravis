package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every test that touches the CLI goes through env: a workspace in
// t.TempDir(), a throwaway HOME, XDG_CONFIG_HOME and APPDATA, and no
// inherited KRAVIS_* variables, so the developer's own actor, configuration
// and workspaces are never read or written.

var (
	// realHome is what HOME was before TestMain poisoned it.
	realHome, _ = os.UserHomeDir()
	// goEnv keeps the Go tool working for the smoke test, which builds the binary.
	goEnv = captureGoEnv()
)

func captureGoEnv() []string {
	out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return nil
	}
	return []string{"GOCACHE=" + lines[0], "GOMODCACHE=" + lines[1]}
}

// TestMain poisons the process-wide HOME for every test of this package: an
// unreadable folder, so a test that skips newEnv and reads or writes the
// real home fails loudly instead of touching it.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	poison, err := os.MkdirTemp("", "kravis-poisoned-home-")
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = os.Chmod(poison, 0o700)
		_ = os.RemoveAll(poison)
	}()
	if err := os.Chmod(poison, 0o000); err != nil {
		panic(err)
	}
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA", "USERPROFILE"} {
		_ = os.Setenv(k, poison)
	}
	dropKravisVars()
	return m.Run()
}

func dropKravisVars() {
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "KRAVIS_") {
			_ = os.Unsetenv(k)
		}
	}
}

type env struct {
	t         *testing.T
	Workspace string
	Home      string
}

type result struct {
	Stdout, Stderr string
	Code           int
}

// newEnv isolates the test and moves it into an empty workspace folder.
func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{t: t, Workspace: filepath.Join(root, "workspace"), Home: filepath.Join(root, "home")}
	for _, d := range []string{e.Workspace, e.Home} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", e.Home)
	t.Setenv("USERPROFILE", e.Home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(e.Home, ".config"))
	t.Setenv("APPDATA", filepath.Join(e.Home, "AppData"))
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "KRAVIS_") {
			t.Setenv(k, "") // registers the restore
			_ = os.Unsetenv(k)
		}
	}
	t.Chdir(e.Workspace)
	return e
}

// run calls the CLI in process with no input.
func (e *env) run(args ...string) result {
	e.t.Helper()
	return e.runWithStdin("", args...)
}

func (e *env) runWithStdin(stdin string, args ...string) result {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(stdin), &stdout, &stderr)
	return result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

// JSON decodes stdout, which must hold exactly one JSON object.
func (r result) JSON(t *testing.T) map[string]any {
	t.Helper()
	return decodeObject(t, r.Stdout)
}

// ErrorJSON decodes stderr, which must hold exactly one JSON object.
func (r result) ErrorJSON(t *testing.T) map[string]any {
	t.Helper()
	return decodeObject(t, r.Stderr)
}

func decodeObject(t *testing.T, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("want one JSON object, got %q: %v", s, err)
	}
	if dec.More() {
		t.Fatalf("want exactly one JSON object, got %q", s)
	}
	return v
}
