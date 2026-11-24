package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/storage"
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
	// Connector, when set, is the storage every run uses instead of the
	// workspace folder: a storage.Memory lets a test force conflicts.
	Connector storage.Connector
	// TTY makes the run believe its stdout is a terminal.
	TTY bool
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
	d := e.deps()
	code := runWith(d, args, strings.NewReader(stdin), &stdout, &stderr)
	return result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

// runRoot runs a command tree the test built, to add commands the CLI does
// not have yet.
func (e *env) runRoot(a *app, root *cobra.Command, args ...string) result {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	code := a.execute(root, args, strings.NewReader(""), &stdout, &stderr)
	return result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

// deps returns the dependencies run uses.
func (e *env) deps() deps {
	d := defaultDeps()
	if e.Connector != nil {
		d.connect = func(string) storage.Connector { return e.Connector }
	}
	d.now = func() time.Time { return time.Date(2026, 11, 24, 9, 30, 0, 0, time.UTC) }
	d.isTTY = func(io.Writer) bool { return e.TTY }
	return d
}

// Files lists every file of the workspace and of the local configuration
// with its SHA-256, to prove what a command did not touch.
func (e *env) Files() map[string]string {
	e.t.Helper()
	out := map[string]string{}
	for label, root := range map[string]string{"ws": e.Workspace, "home": e.Home} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			data, err := os.ReadFile(p)
			if err != nil { // a file the test made unreadable
				out[label+":"+filepath.ToSlash(rel)] = "unreadable"
				return nil
			}
			sum := sha256.Sum256(data)
			out[label+":"+filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
			return nil
		})
		if err != nil {
			e.t.Fatal(err)
		}
	}
	return out
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
