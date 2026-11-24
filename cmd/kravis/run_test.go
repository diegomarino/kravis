package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/diegomarino/kravis/internal/storage"
)

func TestHelperIsolatesTheEnvironment(t *testing.T) {
	t.Setenv("KRAVIS_ACTOR", "us_someone") // would leak without the helper
	e := newEnv(t)

	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA"} {
		got := os.Getenv(k)
		if !strings.HasPrefix(got, e.Home) {
			t.Errorf("%s = %q, want it under the throwaway home %q", k, got, e.Home)
		}
		if realHome != "" && strings.HasPrefix(got, realHome) {
			t.Errorf("%s = %q reaches the real home", k, got)
		}
	}
	if v, ok := os.LookupEnv("KRAVIS_ACTOR"); ok {
		t.Errorf("KRAVIS_ACTOR leaked: %q", v)
	}
	if wd, _ := os.Getwd(); !samePath(wd, e.Workspace) {
		t.Errorf("working folder = %q, want the workspace %q", wd, e.Workspace)
	}
}

func samePath(a, b string) bool {
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra == rb
}

func TestPoisonedHomeRefusesAccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("folder permissions do not apply")
	}
	// No newEnv: this is what a test that forgets the helper would see.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(home); err == nil {
		t.Fatalf("the process HOME %q must be unreadable in tests", home)
	}
}

func TestRunsOnAnInjectedConnector(t *testing.T) {
	e := newEnv(t)
	e.Connector = storage.NewMemory()
	if r := e.run("--version"); r.Code != 0 {
		t.Fatalf("code %d, stderr %q", r.Code, r.Stderr)
	}
	if _, ok := defaultDeps().connect(e.Workspace).(*storage.Local); !ok {
		t.Fatal("the CLI runs on the local filesystem by default")
	}
}

func TestVersionIsJSON(t *testing.T) {
	e := newEnv(t)
	r := e.run("--version")
	if r.Code != 0 || r.Stderr != "" {
		t.Fatalf("code %d, stderr %q", r.Code, r.Stderr)
	}
	data, _ := r.JSON(t)["data"].(map[string]any)
	if data["version"] != "dev" {
		t.Fatalf("version = %v, want dev", data["version"])
	}
}

func TestUsageErrorsAreJSONOnStderr(t *testing.T) {
	e := newEnv(t)
	for _, tt := range []struct {
		args []string
		code string
	}{
		{[]string{"nope"}, "INVALID_VALUE"},
		{[]string{"--nope"}, "UNKNOWN_FIELD"},
	} {
		r := e.run(tt.args...)
		if r.Code != 2 || r.Stdout != "" {
			t.Fatalf("%v: code %d, stdout %q", tt.args, r.Code, r.Stdout)
		}
		errObj, _ := r.ErrorJSON(t)["error"].(map[string]any)
		if errObj["code"] != tt.code {
			t.Errorf("%v: code = %v, want %s", tt.args, errObj["code"], tt.code)
		}
	}
}

func TestHelpIsTextAndBareRunIsJSON(t *testing.T) {
	e := newEnv(t)
	r := e.run("--help")
	if r.Code != 0 || !strings.Contains(r.Stdout, "kravis") {
		t.Errorf("--help: code %d, stdout %q", r.Code, r.Stdout)
	}
	r = e.run()
	if r.Code != 2 || r.Stdout != "" {
		t.Fatalf("bare run: code %d, stdout %q", r.Code, r.Stdout)
	}
	if errObj, _ := r.ErrorJSON(t)["error"].(map[string]any); errObj["code"] != "INVALID_VALUE" {
		t.Errorf("bare run error = %v", errObj)
	}
}

// TestSmokeBinary builds the real binary and runs it once.
func TestSmokeBinary(t *testing.T) {
	pkgDir, err := os.Getwd() // the package folder, before newEnv leaves it
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	bin := filepath.Join(t.TempDir(), "kravis")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".")
	build.Env = append(os.Environ(), goEnv...)
	build.Dir = pkgDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "--version")
	cmd.Dir = e.Workspace
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeObject(t, string(out)); got["data"] == nil {
		t.Fatalf("got %s", out)
	}
}
