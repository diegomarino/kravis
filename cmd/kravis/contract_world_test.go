package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/diegomarino/kravis/internal/storage"
)

// The states a contract case runs in. A state is a base world plus an
// optional change made on its copy; the bases are built once per test run and
// copied for each case, so cases are independent and cheap. The cast is
// Dunder Mifflin Scranton, with every address on a .example domain.
//
// Task 09 (golden snapshots) reuses this file and contract_cases_test.go: a
// case is data, and newContractWorlds(t).instance(t, state) gives the
// workspace it runs in.

// Base worlds.
const (
	baseNone   = "none"   // an empty folder: no workspace
	baseInit   = "init"   // init only: Pam, the one user, the default actor
	baseDunder = "dunder" // the whole cast
)

// vars are the IDs a world cannot spell in advance (TypeIDs) and the cursor a
// Before step captured.
type vars map[string]string

// stateDef is a base world and the change a case needs on top of it.
type stateDef struct {
	base   string
	change func(t *testing.T, e *env, v vars)
}

// worlds holds the built base worlds.
type worlds struct {
	dirs map[string]string // base -> folder holding ws/ and home/
	vars map[string]vars
}

// newContractWorlds builds every base world under t.TempDir().
func newContractWorlds(t *testing.T) *worlds {
	t.Helper()
	w := &worlds{dirs: map[string]string{}, vars: map[string]vars{}}
	for _, base := range []string{baseNone, baseInit, baseDunder} {
		e := newEnv(t)
		v := vars{}
		switch base {
		case baseInit:
			seedInit(e)
		case baseDunder:
			v = seedDunder(e)
		}
		dir := t.TempDir()
		copyTree(t, e.Workspace, filepath.Join(dir, "ws"))
		copyTree(t, e.Home, filepath.Join(dir, "home"))
		w.dirs[base], w.vars[base] = dir, v
	}
	return w
}

// instance returns a fresh env holding a copy of the state's base world, with
// the state's change applied, and the world's variables.
func (w *worlds) instance(t *testing.T, state string) (*env, vars) {
	t.Helper()
	def, ok := contractStates[state]
	if !ok {
		t.Fatalf("no state %q", state)
	}
	e := newEnv(t)
	copyTree(t, filepath.Join(w.dirs[def.base], "ws"), e.Workspace)
	copyTree(t, filepath.Join(w.dirs[def.base], "home"), e.Home)
	v := vars{"ws": e.Workspace}
	for k, val := range w.vars[def.base] {
		v[k] = val
	}
	if def.change != nil {
		def.change(t, e, v)
	}
	return e, v
}

// copyTree copies the files of src into dst, creating folders as needed.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func seedInit(e *env) {
	e.t.Helper()
	if r := e.run(initArgs...); r.Code != 0 {
		e.t.Fatalf("seed init: code %d, stderr %q", r.Code, r.Stderr)
	}
}

// initArgs is the init every world starts with: Pam Beesly, the first human
// of Dunder Mifflin Scranton.
var initArgs = []string{"init", "--timezone", "America/New_York", "--organization", "Dunder Mifflin Scranton",
	"--user", "pam", "--user-name", "Pam Beesly", "--user-email", "pam.beesly@dundermifflin.example"}

// seedDunder builds the cast and returns the IDs it cannot spell.
func seedDunder(e *env) vars {
	t := e.t
	t.Helper()
	ok := func(stdin string, args ...string) map[string]any {
		t.Helper()
		r := e.runWithStdin(stdin, args...)
		if r.Code != 0 || r.Stderr != "" {
			t.Fatalf("seed %v: code %d, stderr %q", args, r.Code, r.Stderr)
		}
		return r.JSON(t)
	}
	id := func(out map[string]any) string { return str(at(out, "data", "id")) }
	holder := "org_dunder-mifflin-scranton"
	v := vars{}

	ok("", initArgs...)
	ok("", "user", "create", "michael", "--human", "--holder", holder)
	ok("", "user", "create", "kevin", "--human", "--holder", holder)
	ok("", "person", "create", "--name", "Toby Flenderson", "--email", "toby.flenderson@dundermifflin.example")
	ok("", "organization", "create", "--name", "Sabre", "--domain", "sabre.example")
	ok("", "user", "create", "pam-agent", "--agent", "--holder", holder, "--handler", "us_pam")
	ok("", "person", "create", "--name", "Michael Scott", "--email", "michael.scott@dundermifflin.example")
	ok("", "user", "update", "us_michael", "--person", "pe_michael-scott")
	for _, p := range [][2]string{{"Jim Halpert", "jim.halpert"}, {"Dwight Schrute", "dwight.schrute"},
		{"Angela Martin", "angela.martin"}, {"Jan Levinson", "jan.levinson"}, {"Bob Vance", "bob.vance"}} {
		ok("", "person", "create", "--name", p[0], "--email", p[1]+"@dundermifflin.example")
	}
	ok("", "organization", "create", "--name", "Dunder Mifflin Corporate", "--domain", "corporate.dundermifflin.example")
	ok("", "organization", "create", "--name", "Michael Scott Paper Company", "--domain", "mspc.example")
	ok("", "organization", "create", "--name", "Vance Refrigeration", "--domain", "vance.example")
	ok("", "organization", "create", "--name", "Prince Family Paper")
	for _, a := range [][3]string{
		{"pe_jan-levinson", "org_dunder-mifflin-corporate", "VP of Sales"},
		{"pe_bob-vance", "org_vance-refrigeration", "Owner"},
		{"pe_jim-halpert", "org_michael-scott-paper-company", "Salesman"},
		{"pe_angela-martin", "org_dunder-mifflin-corporate", "Accountant"},
		{"pe_angela-martin", "org_vance-refrigeration", "Consultant"},
	} {
		ok("", "person", "affiliation", "add", a[0], a[1], "--title", a[2])
	}

	// A second holder, so a link across holders can be refused.
	ok("", "organization", "create", "--name", "Schrute Farms", "--own", "--domain", "schrute-farms.example")
	ok("", "user", "create", "dwight", "--human", "--holder", "org_schrute-farms")
	ok("", "--actor", "us_dwight", "deal", "create", "--party", "pe_dwight-schrute", "--holder-role", "advisor")
	ok("", "--actor", "us_dwight", "project", "create", "--name", "Beet harvest")

	// Interactions, with a follow-up on the first.
	ok("Jim and Dwight walked through the Q4 pipeline.\nDwight wants the beet account flagged.\n",
		"interaction", "create", "--channel", "email", "--occurred-at", "2026-11-10T14:00:00Z",
		"--subject", "Q4 pipeline review", "--participant", "pe_jim-halpert", "--participant", "pe_dwight-schrute",
		"--ref", "email:message:pipeline-review-1@dundermifflin.example", "--initiated-by", "us_pam", "--summary-file", "-")
	ok("Jan asked for the Scranton branch numbers.\n",
		"interaction", "create", "--channel", "email", "--occurred-at", "2026-11-12T09:15:00Z", "--subject", "Branch numbers",
		"--participant", "pe_jan-levinson", "--direction", "inbound", "--initiated-by", "pe_jan-levinson",
		"--ref", "email:message:branch-numbers@dundermifflin.example", "--summary-file", "-")
	v["task"] = id(ok("", "task", "create", "--title", "Send Dwight the beet account terms", "--assignee", "us_pam",
		"--agent", "us_pam-agent", "--interaction", "it_2026-11-10-q4-pipeline-review", "--due", "2026-11-20"))

	// Deals, projects, tasks and notes.
	ok("", "deal", "create", "--party", "org_michael-scott-paper-company", "--role", "seller",
		"--holder-role", "advisor", "--name", "Acquisition of Michael Scott Paper Company")
	ok("", "deal", "party", "add", "dl_michael-scott-paper-company", "org_dunder-mifflin-corporate", "--role", "buyer")
	ok("", "deal", "party", "add", "dl_michael-scott-paper-company", "pe_michael-scott", "--role", "client")
	ok("", "deal", "stage", "dl_michael-scott-paper-company", "engaged")
	ok("", "deal", "create", "--party", "org_dunder-mifflin-corporate", "--holder-role", "advisor")
	ok("Michael pitched the buyout in the conference room.\n",
		"interaction", "create", "--channel", "meeting", "--occurred-at", "2026-11-17T15:00:00Z", "--subject", "Pitch to Corporate",
		"--participant", "pe_michael-scott", "--deal", "dl_michael-scott-paper-company", "--initiated-by", "us_pam", "--summary-file", "-")
	v["task3"] = id(ok("", "task", "create", "--title", "Draft the term sheet", "--assignee", "us_michael",
		"--interaction", "it_2026-11-17-pitch-to-corporate", "--due", "2026-11-25"))
	ok("", "project", "create", "--name", "Paper stock audit", "--deal", "dl_michael-scott-paper-company")
	v["task2"] = id(ok("", "task", "create", "--title", "Count the ream pallets", "--assignee", "us_pam",
		"--project", "pr_paper-stock-audit", "--due", "2026-12-01"))
	ok("", "project", "create", "--name", "Warehouse move")
	ok("", "project", "close", "pr_warehouse-move")
	ok("", "project", "create", "--name", "Desk move")
	ok("", "project", "create", "--name", "Break room refit")
	v["note"] = id(ok("Prince Family Paper is a rival for the Scranton accounts.\nWatch Bob Vance too.\n",
		"note", "create", "--subject", "org_prince-family-paper", "--subject", "pe_bob-vance", "--text-file", "-"))
	v["note2"] = id(ok("Angela thinks the price is too high.\n",
		"note", "create", "--subject", "dl_michael-scott-paper-company", "--subject", "pe_angela-martin", "--text-file", "-"))
	return v
}

// editRecord rewrites the JSON record at rel (relative to the workspace)
// after change edits its fields.
func editRecord(t *testing.T, e *env, rel string, change func(map[string]any)) {
	t.Helper()
	p := filepath.Join(e.Workspace, filepath.FromSlash(rel))
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	change(rec)
	out, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, e *env, rel, text string) {
	t.Helper()
	p := filepath.Join(e.Workspace, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// conflictConnector refuses every replace as if another writer had won, so a
// write retries and gives up.
type conflictConnector struct{ storage.Connector }

func (c conflictConnector) Replace(string, []byte, storage.Token) (storage.Token, error) {
	return "", storage.ErrTokenMismatch
}

// contractStates are the states cases name. A damaged workspace has the one
// fault its check finding is about.
var contractStates = map[string]stateDef{
	baseNone:   {base: baseNone},
	baseInit:   {base: baseInit},
	baseDunder: {base: baseDunder},

	// A workspace whose machine has no default actor.
	"dunder, no default actor": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		if err := os.Remove(filepath.Join(e.Home, filepath.FromSlash(localConfigRel()))); err != nil {
			t.Fatal(err)
		}
	}},
	// Every replace loses to another writer.
	"dunder, writers keep racing": {base: baseDunder, change: func(_ *testing.T, e *env, _ vars) {
		e.Connector = conflictConnector{storage.NewLocal(e.Workspace)}
	}},
	// The storage answers with an unexpected failure.
	"dunder, storage fails": {base: baseDunder, change: func(_ *testing.T, e *env, _ vars) {
		e.Connector = errConnector{storage.NewLocal(e.Workspace), errors.New("disk on fire")}
	}},
	// The storage is momentarily unavailable.
	"dunder, storage unavailable": {base: baseDunder, change: func(_ *testing.T, e *env, _ vars) {
		e.Connector = errConnector{storage.NewLocal(e.Workspace), storage.ErrUnavailable}
	}},

	"damaged: config.json missing": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		if err := os.Remove(filepath.Join(e.Workspace, "config.json")); err != nil {
			t.Fatal(err)
		}
	}},
	"damaged: config.json invalid": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		writeFile(t, e, "config.json", "{ not json")
	}},
	"damaged: orphan sidecar": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		writeFile(t, e, "records/deals/dl_ghost.md", "Nobody's notes.\n")
	}},
	"damaged: unreadable record": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		p := filepath.Join(e.Workspace, "records", "people", "pe_toby-flenderson.json")
		if err := os.Chmod(p, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	}},
	"damaged: malformed record": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		writeFile(t, e, "records/people/pe_garbage.json", "this is not a record")
	}},
	"damaged: invalid record": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		editRecord(t, e, "records/people/pe_bob-vance.json", func(r map[string]any) { r["version"] = 0 })
	}},
	"damaged: key without value": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		editRecord(t, e, "records/people/pe_toby-flenderson.json", func(r map[string]any) { delete(r, "emails") })
	}},
	"damaged: value without key": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		editRecord(t, e, "records/organizations/org_prince-family-paper.json", func(r map[string]any) {
			r["domains"] = []any{"prince.example"}
		})
	}},
	"damaged: broken reference": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		editRecord(t, e, "records/users/us_michael.json", func(r map[string]any) { r["person"] = "pe_nobody" })
	}},
	"damaged: dangling reference": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		editRecord(t, e, "records/interactions/2026-11/it_2026-11-10-q4-pipeline-review.json", func(r map[string]any) {
			r["projects"] = []any{"pr_nowhere"}
		})
	}},
	"damaged: value not in catalog": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		editRecord(t, e, "records/interactions/2026-11/it_2026-11-12-branch-numbers.json", func(r map[string]any) {
			r["channel"] = "carrier-pigeon"
		})
	}},
	"damaged: holder mismatch": {base: baseDunder, change: func(t *testing.T, e *env, _ vars) {
		editRecord(t, e, "records/interactions/2026-11/it_2026-11-17-pitch-to-corporate.json", func(r map[string]any) {
			r["deals"] = []any{"dl_dwight-schrute"}
		})
	}},
}
