package specgate

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const additiveModel = `{"added":[{"id":"WORK-AC-03","kind":"Guarantee"}],"removed":[],"relocated":[],
"changed":[
 {"id":"domain:work","kind":"Domain","changes":[{"path":"/guarantees","beforePresent":true,"afterPresent":true,"before":["WORK-AC-01"],"after":["WORK-AC-01","WORK-AC-03"]}]},
 {"id":"concept:task","kind":"Concept","changes":[
  {"path":"/purpose","beforePresent":true,"afterPresent":true,"before":"A piece of work.","after":"A piece of work. It has a status."},
  {"path":"/notes","beforePresent":false,"afterPresent":true,"after":"new"}]}]}`

const changedGuarantee = `{"added":[],"removed":[],"relocated":[],
"changed":[{"id":"WORK-AC-01","kind":"Guarantee","changes":[{"path":"/statement","beforePresent":true,"afterPresent":true,"before":"a","after":"b"}]}]}`

func TestClassifyModelDiff(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string // substring of the first reason; empty means additive
	}{
		{"additive", additiveModel, ""},
		{"changed guarantee", changedGuarantee, "Guarantee WORK-AC-01 was changed"},
		{"changed relationship", strings.ReplaceAll(changedGuarantee, "Guarantee", "Relationship"), "Relationship"},
		{"removed", `{"removed":[{"id":"WORK-AC-02","kind":"Guarantee"}]}`, "WORK-AC-02 was removed"},
		{"relocated", `{"relocated":[{"id":"concept:task","kind":"Concept"}]}`, "was relocated"},
		{"text shortened", `{"changed":[{"id":"concept:task","kind":"Concept","changes":[{"path":"/purpose","beforePresent":true,"afterPresent":true,"before":"long text","after":"text"}]}]}`, "/purpose"},
		{"field dropped", `{"changed":[{"id":"concept:task","kind":"Concept","changes":[{"path":"/purpose","beforePresent":true,"afterPresent":false,"before":"x"}]}]}`, "/purpose"},
		{"list item dropped", `{"changed":[{"id":"domain:work","kind":"Domain","changes":[{"path":"/g","beforePresent":true,"afterPresent":true,"before":["a","b"],"after":["a"]}]}]}`, "/g"},
		{"list became string", `{"changed":[{"id":"x","kind":"Concept","changes":[{"path":"/g","beforePresent":true,"afterPresent":true,"before":["a"],"after":"a"}]}]}`, "/g"},
		{"string became list", `{"changed":[{"id":"x","kind":"Concept","changes":[{"path":"/g","beforePresent":true,"afterPresent":true,"before":"a","after":["a"]}]}]}`, "/g"},
		{"number changed", `{"changed":[{"id":"x","kind":"Concept","changes":[{"path":"/n","beforePresent":true,"afterPresent":true,"before":1,"after":2}]}]}`, "/n"},
		{"number kept", `{"changed":[{"id":"x","kind":"Concept","changes":[{"path":"/n","beforePresent":true,"afterPresent":true,"before":1,"after":1}]}]}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClassifyModelDiff([]byte(tt.doc))
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" && len(got) != 0 {
				t.Fatalf("want additive, got %v", got)
			}
			if tt.want != "" && (len(got) == 0 || !strings.Contains(got[0], tt.want)) {
				t.Fatalf("want a reason containing %q, got %v", tt.want, got)
			}
		})
	}
	if _, err := ClassifyModelDiff([]byte("not json")); err == nil {
		t.Fatal("want an error for invalid JSON")
	}
}

func TestClassifyLineDiff(t *testing.T) {
	added := "diff --git a/docs/cli.md b/docs/cli.md\n--- a/docs/cli.md\n+++ b/docs/cli.md\n@@ -3,0 +4 @@\n+a new line\n"
	if got := ClassifyLineDiff(added); len(got) != 0 {
		t.Fatalf("a gained line is additive, got %v", got)
	}
	changed := "--- a/docs/cli.md\n+++ b/docs/cli.md\n@@ -3 +3 @@\n-old line\n+new line\n"
	got := ClassifyLineDiff(changed)
	if len(got) != 1 || !strings.Contains(got[0], "docs/cli.md lost a line: old line") {
		t.Fatalf("a changed line needs the person, got %v", got)
	}
	// A removed Markdown bullet and a removed "-- " line look like headers.
	list := "--- a/GLOSSARY.md\n+++ b/GLOSSARY.md\n@@ -1 +0,0 @@\n-- item\n--- rule\n"
	if got := ClassifyLineDiff(list); len(got) != 2 {
		t.Fatalf("want 2 removed lines, got %v", got)
	}
}

func TestClassifyNameStatus(t *testing.T) {
	if got := ClassifyNameStatus("\n"); len(got) != 0 {
		t.Fatalf("no decision change is additive, got %v", got)
	}
	got := ClassifyNameStatus("A\tdocs/ddd/decisions/ADR-002.md\nM\tdocs/ddd/decisions/ADR-001.md\n")
	if len(got) != 2 {
		t.Fatalf("want 2 reasons, got %v", got)
	}
}

func TestClassifyContractFiles(t *testing.T) {
	if got := ClassifyContractFiles("A\tinternal/storage/testdata/golden/person.json\n"); len(got) != 0 {
		t.Fatalf("a new golden file is additive, got %v", got)
	}
	got := ClassifyContractFiles("M\tinternal/storage/testdata/golden/task.json\nD\tinternal/storage/testdata/golden/x.json\nR100\tinternal/storage/testdata/golden/a.json\tinternal/storage/testdata/golden/b.json\n")
	if len(got) != 3 || !strings.Contains(got[0], "contract file changed: M internal/storage/testdata/golden/task.json") {
		t.Fatalf("changed, removed and renamed golden files need the person, got %v", got)
	}
}

func TestMainContractFiles(t *testing.T) {
	const golden = "internal/storage/testdata/golden"
	added := map[string]string{golden: "A\t" + golden + "/person.json\n"}
	if code, out := runMain(t, added, "", "--base", "main"); code != ExitAdditive {
		t.Fatalf("a new golden file: want additive, got %d: %s", code, out)
	}
	changed := map[string]string{golden: "M\t" + golden + "/task.json\n"}
	code, out := runMain(t, changed, "", "--base", "main")
	if code != ExitNeedsPerson || !strings.Contains(out, "task.json") {
		t.Fatalf("a changed golden file: want needs-person, got %d: %s", code, out)
	}
	if code, _ := runMain(t, changed, "", "--base", "main", "--approved"); code != ExitAdditive {
		t.Fatalf("the label lets a changed golden file through, got %d", code)
	}
	if code, _ := runMain(t, nil, golden, "--base", "main"); code != ExitFailure {
		t.Fatal("a failing contract diff is a failure")
	}
}

func TestClassifySubjects(t *testing.T) {
	subjects := []string{"feat(cli): add init", "fix!: drop a field", "refactor(storage)!: rename", "docs: say wow! now", ""}
	if got := ClassifySubjects(subjects); len(got) != 2 {
		t.Fatalf("want the 2 breaking subjects, got %v", got)
	}
}

// fakeRunner answers the commands of Main from a table keyed by the command
// line. Unknown commands succeed with no output.
func fakeRunner(answers map[string]string, fail string) Runner {
	return func(name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		if fail != "" && strings.Contains(line, fail) {
			return nil, errors.New("boom")
		}
		for key, out := range answers {
			if strings.Contains(line, key) {
				return []byte(out), nil
			}
		}
		return nil, nil
	}
}

func runMain(t *testing.T, answers map[string]string, fail string, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Main(args, &out, &errOut, fakeRunner(answers, fail))
	return code, out.String() + errOut.String()
}

func TestMainAdditiveSample(t *testing.T) {
	answers := map[string]string{
		"git ls-tree":  "docs/ddd\n",
		"ddduck diff":  additiveModel,
		"git diff -U0": "--- a/docs/cli.md\n+++ b/docs/cli.md\n+one more line\n",
		"git log":      "feat(cli): add init\n",
		"name-status":  "",
	}
	code, out := runMain(t, answers, "", "--base", "origin/main")
	if code != ExitAdditive || !strings.Contains(out, "additive change") {
		t.Fatalf("want additive, got %d: %s", code, out)
	}
}

func TestMainChangedGuaranteeNeedsLabel(t *testing.T) {
	answers := map[string]string{"git ls-tree": "docs/ddd\n", "ddduck diff": changedGuarantee}
	code, out := runMain(t, answers, "", "--base", "origin/main")
	if code != ExitNeedsPerson || !strings.Contains(out, "WORK-AC-01") {
		t.Fatalf("want needs-person, got %d: %s", code, out)
	}
	code, out = runMain(t, answers, "", "--base", "origin/main", "--approved")
	if code != ExitAdditive || !strings.Contains(out, "approved") {
		t.Fatalf("the label lets it through, got %d: %s", code, out)
	}
}

func TestMainOtherReasons(t *testing.T) {
	for name, answers := range map[string]map[string]string{
		"decision": {"name-status": "M\tdocs/ddd/decisions/ADR-001.md\n"},
		"line":     {"git diff -U0": "--- a/GLOSSARY.md\n+++ b/GLOSSARY.md\n-gone\n"},
		"bang":     {"git log": "feat!: break\n"},
	} {
		if code, out := runMain(t, answers, "", "--base", "main"); code != ExitNeedsPerson {
			t.Errorf("%s: want needs-person, got %d: %s", name, code, out)
		}
	}
}

func TestMainWithoutModelAtBase(t *testing.T) {
	// ls-tree prints nothing: the base has no model, so ddduck is not run.
	code, out := runMain(t, map[string]string{"ddduck diff": changedGuarantee}, "", "--base", "main")
	if code != ExitAdditive {
		t.Fatalf("got %d: %s", code, out)
	}
}

func TestMainFailures(t *testing.T) {
	if code, _ := runMain(t, nil, ""); code != ExitFailure {
		t.Error("a missing --base is a failure")
	}
	if code, _ := runMain(t, nil, "", "--nope"); code != ExitFailure {
		t.Error("a bad flag is a failure")
	}
	answers := map[string]string{"git ls-tree": "docs/ddd\n"}
	for _, fail := range []string{"name-status", "git diff -U0", "git log", "git ls-tree", "git archive", "tar -xf", "ddduck"} {
		if code, out := runMain(t, answers, fail, "--base", "main"); code != ExitFailure || !strings.Contains(out, "boom") {
			t.Errorf("%s failing: want failure, got %d: %s", fail, code, out)
		}
	}
	if code, _ := runMain(t, map[string]string{"git ls-tree": "docs/ddd\n", "ddduck diff": "not json"}, "", "--base", "main"); code != ExitFailure {
		t.Error("invalid ddduck output is a failure")
	}
}

func TestExecRunner(t *testing.T) {
	out, err := ExecRunner("go", "env", "GOOS")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		t.Fatalf("got %q, %v", out, err)
	}
	if _, err := ExecRunner("go", "no-such-subcommand"); err == nil {
		t.Fatal("want an error")
	}
}
