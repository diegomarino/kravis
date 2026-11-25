package repocheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// licenseName matches the files that hold a module's license or notice.
var licenseName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice)`)

// squash makes the comparison immune to line wrapping and spacing.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestReleaseShipsEveryDependencyNotice: every module compiled into the
// binary has its license and notice texts in THIRD_PARTY_NOTICES.md, and the
// release archives carry that file and LICENSE.
func TestReleaseShipsEveryDependencyNotice(t *testing.T) {
	top := root(t)
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(top, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return string(b)
	}

	if !strings.HasPrefix(read("LICENSE"), "MIT License") {
		t.Error("LICENSE is not the MIT license")
	}
	releaser := read(".goreleaser.yaml")
	for _, f := range []string{"LICENSE", "THIRD_PARTY_NOTICES.md"} {
		if !strings.Contains(releaser, f) {
			t.Errorf(".goreleaser.yaml does not put %s in the archives", f)
		}
	}

	notices := squash(read("THIRD_PARTY_NOTICES.md"))
	lower := strings.ToLower(notices)
	if !strings.Contains(lower, "## go standard library") {
		t.Error("THIRD_PARTY_NOTICES.md has no section for the Go standard library")
	}

	// The binary builds for the release's operating systems; the union of
	// their dependencies is what ships.
	mods := map[string]string{}
	for _, goos := range []string{"darwin", "linux"} {
		cmd := exec.Command("go", "list", "-deps", "-f",
			"{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}", "./cmd/kravis")
		cmd.Dir = top
		cmd.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list (%s): %v", goos, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f := strings.Fields(line); len(f) == 3 {
				mods[f[0]+" "+f[1]] = f[2]
			}
		}
	}
	if len(mods) == 0 {
		t.Fatal("no dependencies found")
	}
	for mod, dir := range mods {
		if !strings.Contains(notices, "## "+mod) {
			t.Errorf("THIRD_PARTY_NOTICES.md has no section for %s", mod)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: %v", mod, err)
		}
		found := false
		for _, e := range entries {
			if e.IsDir() || !licenseName.MatchString(e.Name()) {
				continue
			}
			found = true
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(notices, squash(string(b))) {
				t.Errorf("THIRD_PARTY_NOTICES.md lacks the text of %s in %s", e.Name(), mod)
			}
		}
		if !found {
			t.Errorf("%s has no license file", mod)
		}
	}
}
