// Package repocheck holds the checks that cover the whole repository, not one
// package: nothing personal in tracked files, and the inventory of the
// acceptance criteria and invariants in scope.
package repocheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// root returns the repository's top folder.
func root(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func trackedFiles(t *testing.T, top string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = top
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// --- Nothing personal in the repository ---

var (
	homePath = regexp.MustCompile(`(?:/Users/|/home/)[A-Za-z0-9_.-]+|C:\\Users\\[A-Za-z0-9_.-]+`)
	email    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,})`)
	okDomain = []string{"example.com", "example.org", "example.net"}
)

// personal lists what in text points at a real person or machine.
func personal(text string) []string {
	var found []string
	found = append(found, homePath.FindAllString(text, -1)...)
	for _, m := range email.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(okDomain, strings.ToLower(m[1])) {
			found = append(found, m[0])
		}
	}
	return found
}

func TestNothingPersonalInTrackedFiles(t *testing.T) {
	top := root(t)
	for _, f := range trackedFiles(t, top) {
		data, err := os.ReadFile(filepath.Join(top, f))
		if os.IsNotExist(err) {
			continue // staged for deletion
		}
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(data, 0) {
			continue // binary
		}
		for _, p := range personal(string(data)) {
			t.Errorf("%s: personal data %q", f, p)
		}
	}
}

func TestPersonalDetector(t *testing.T) {
	bad := []string{
		"see /Us" + "ers/alice/project",
		"in /ho" + "me/bob/.config",
		`C:\Us` + `ers\carol\Documents`,
		"write to alice@" + "gmail.com",
		"write to Bob@Example." + "co",
	}
	for _, s := range bad {
		if len(personal(s)) == 0 {
			t.Errorf("%q should be flagged", s)
		}
	}
	good := []string{
		"ana@example.com, ben@example.org, cy@EXAMPLE.net",
		"~/.config/kravis and /Users/<name> and /home/<name>",
		"npm i @types/node and git@github",
	}
	for _, s := range good {
		if got := personal(s); len(got) != 0 {
			t.Errorf("%q should pass, flagged %v", s, got)
		}
	}
}

// --- Inventory of acceptance criteria and invariants ---

var (
	testFunc = regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	modelID  = regexp.MustCompile(`(?m)^id: (\S+)\s*$`)
)

// readScope reads one ID per line, ignoring blanks and # comments.
func readScope(text string) []string {
	var ids []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			ids = append(ids, line)
		}
	}
	return ids
}

// inventoryProblems reports each in-scope ID, an acceptance criterion or an
// invariant, with no test naming it (WORKSPACE-AC-04 -> TestWORKSPACE_AC_04...,
// WORKSPACE-INV-07 -> TestWORKSPACE_INV_07...) or not in the model.
func inventoryProblems(scope, tests, model []string) []string {
	var problems []string
	for _, id := range scope {
		if !slices.Contains(model, id) {
			problems = append(problems, id+": not a guarantee of docs/ddd/model/guarantees/")
		}
		want := "Test" + strings.ReplaceAll(id, "-", "_")
		if !slices.ContainsFunc(tests, func(name string) bool { return strings.Contains(name, want) }) {
			problems = append(problems, id+": no test whose name contains "+want)
		}
	}
	return problems
}

func modelGuaranteeIDs(t *testing.T, top string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(top, "docs", "ddd", "model", "guarantees", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no guarantees found: %v", err)
	}
	var ids []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := modelID.FindSubmatch(data); m != nil {
			ids = append(ids, string(m[1]))
		}
	}
	return ids
}

func testNames(t *testing.T, top string) []string {
	t.Helper()
	var names []string
	for _, f := range trackedFiles(t, top) {
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(top, f))
		if err != nil {
			continue
		}
		for _, m := range testFunc.FindAllSubmatch(data, -1) {
			names = append(names, string(m[1]))
		}
	}
	return names
}

func TestAcceptanceCriteriaInventory(t *testing.T) {
	top := root(t)
	data, err := os.ReadFile("scope.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range inventoryProblems(readScope(string(data)), testNames(t, top), modelGuaranteeIDs(t, top)) {
		t.Error(p)
	}
}

func TestInventoryDetector(t *testing.T) {
	model := []string{"WORKSPACE-AC-04", "WORK-AC-01"}
	tests := []string{"TestWORKSPACE_AC_04_InitCreates", "TestOther"}

	if got := inventoryProblems([]string{"WORKSPACE-AC-04"}, tests, model); len(got) != 0 {
		t.Errorf("a covered, real ID passes, got %v", got)
	}
	if got := inventoryProblems([]string{"WORK-AC-01"}, tests, model); len(got) != 1 || !strings.Contains(got[0], "TestWORK_AC_01") {
		t.Errorf("an ID with no test fails, got %v", got)
	}
	if got := inventoryProblems([]string{"NOPE-AC-99"}, []string{"TestNOPE_AC_99"}, model); len(got) != 1 || !strings.Contains(got[0], "not a guarantee") {
		t.Errorf("an invented ID fails, got %v", got)
	}
	if got := readScope("# c\n\n WORK-AC-01 \nX-AC-02\n"); !slices.Equal(got, []string{"WORK-AC-01", "X-AC-02"}) {
		t.Errorf("readScope = %v", got)
	}
}

func TestInventoryDetectorTakesInvariants(t *testing.T) {
	model := []string{"WORKSPACE-INV-07", "WORKSPACE-INV-08"}
	tests := []string{"TestWORKSPACE_INV_07_IDNeverChanges"}

	if got := inventoryProblems([]string{"WORKSPACE-INV-07"}, tests, model); len(got) != 0 {
		t.Errorf("a covered, real invariant passes, got %v", got)
	}
	if got := inventoryProblems([]string{"WORKSPACE-INV-08"}, tests, model); len(got) != 1 || !strings.Contains(got[0], "TestWORKSPACE_INV_08") {
		t.Errorf("a listed invariant with no test fails, got %v", got)
	}
	if got := inventoryProblems([]string{"WORKSPACE-INV-99"}, []string{"TestWORKSPACE_INV_99"}, model); len(got) != 1 || !strings.Contains(got[0], "not a guarantee") {
		t.Errorf("an invented invariant fails, got %v", got)
	}
}

func TestModelIDsAreReadable(t *testing.T) {
	ids := modelGuaranteeIDs(t, root(t))
	for _, id := range []string{"WORK-AC-01", "WORKSPACE-INV-07"} {
		if !slices.Contains(ids, id) {
			t.Fatalf("%s missing from %v", id, ids)
		}
	}
}
