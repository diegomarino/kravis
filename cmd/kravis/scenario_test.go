package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestScenarioDunderMifflinScranton runs the built binary, one process per
// command, through a workspace like the one the skill teaches: the recipes
// for interactions, deals, projects, notes, briefings, outreach, search and
// corrections. Each step says what it asserts. The cast is fiction.
func TestScenarioDunderMifflinScranton(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	bin := filepath.Join(t.TempDir(), "kravis")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".")
	build.Env = append(os.Environ(), goEnv...)
	build.Dir = pkgDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	s := &scenario{t: t, e: e, bin: bin}
	started := time.Now()
	defer func() { t.Logf("scenario: %d commands in %s", s.n, time.Since(started).Round(time.Millisecond)) }()

	holder := "org_dunder-mifflin-scranton"

	// --- The workspace and its people -------------------------------------
	d := s.ok("created", "init", "--timezone", "America/New_York", "--organization", "Dunder Mifflin Scranton",
		"--user", "pam", "--user-name", "Pam Beesly", "--user-email", "pam.beesly@example.com")
	s.eq(at(d, "data", "user", "person"), "pe_pam-beesly", "init links Pam to her person")
	d = s.ok("", "whoami")
	s.eq(at(d, "data", "actor"), "us_pam", "whoami: the default actor is Pam")
	s.eq(at(d, "data", "holder"), holder, "whoami: her holder")

	s.ok("created", "user", "create", "michael", "--human", "--holder", holder)
	s.ok("created", "user", "create", "pam-agent", "--agent", "--holder", holder, "--handler", "us_pam")
	s.ok("created", "person", "create", "--name", "Michael Scott", "--email", "michael.scott@example.com")
	d = s.ok("updated", "user", "update", "us_michael", "--person", "pe_michael-scott")
	s.eq(at(d, "data", "person"), "pe_michael-scott", "Michael is linked to his person")

	people := map[string]string{
		"Jim Halpert": "jim.halpert", "Dwight Schrute": "dwight.schrute", "Angela Martin": "angela.martin",
		"Jan Levinson": "jan.levinson", "David Wallace": "david.wallace", "Bob Vance": "bob.vance",
	}
	for name, local := range people {
		s.ok("created", "person", "create", "--name", name, "--email", local+"@example.com")
	}
	s.eq(at(s.ok("existing", "person", "create", "--name", "Jim Halpert", "--email", "JIM.HALPERT@example.com"), "data", "id"),
		"pe_jim-halpert", "a person is found again by email, whatever its case")

	s.ok("created", "organization", "create", "--name", "Dunder Mifflin Corporate", "--domain", "corporate.example.com")
	s.ok("created", "organization", "create", "--name", "Prince Family Paper")
	s.ok("created", "organization", "create", "--name", "Michael Scott Paper Company", "--domain", "mspc.example.com")
	s.ok("created", "organization", "create", "--name", "Vance Refrigeration", "--domain", "vance.example.com")
	d = s.ok("created", "organization", "create", "--name", "Prince Family Paper")
	s.eq(at(d, "data", "id"), "org_prince-family-paper-2", "an organization without a domain has no natural key")
	s.eq(at(d, "warnings", 0, "code"), "SIMILAR_EXISTS", "warning on the repeated name")
	s.eq(at(d, "warnings", 0, "details", "id"), "org_prince-family-paper", "the warning names the existing one")
	s.ok("deleted", "organization", "delete", "org_prince-family-paper-2")

	s.ok("created", "person", "affiliation", "add", "pe_jan-levinson", "org_dunder-mifflin-corporate", "--title", "VP of Sales")
	s.ok("created", "person", "affiliation", "add", "pe_david-wallace", "org_dunder-mifflin-corporate", "--title", "CFO")
	s.ok("created", "person", "affiliation", "add", "pe_bob-vance", "org_vance-refrigeration", "--title", "Owner")
	s.ok("created", "person", "affiliation", "add", "pe_jim-halpert", "org_michael-scott-paper-company", "--title", "Salesman")
	d = s.ok("updated", "person", "affiliation", "add", "pe_bob-vance", "org_vance-refrigeration", "--title", "Founder")
	s.eq(at(d, "data", "affiliations", 0, "title"), "Founder", "an affiliation on an existing pair updates the title")

	// --- Interactions ------------------------------------------------------
	pipeline := []string{"interaction", "create", "--channel", "email", "--occurred-at", "2026-11-10T14:00:00Z",
		"--subject", "Q4 pipeline review", "--participant", "pe_jim-halpert", "--participant", "pe_dwight-schrute",
		"--ref", "email:message:pipeline-review-1@example.com", "--ref", "gmail:thread:thread-18c0ffee@pam",
		"--initiated-by", "us_pam", "--summary-file", "-"}
	summary := "Jim and Dwight walked through the Q4 pipeline.\nDwight wants the Schrute Farms beet account flagged.\n"
	d = s.okIn(summary, "created", pipeline...)
	pipelineID := str(at(d, "data", "id"))
	s.eq(pipelineID, "it_2026-11-10-q4-pipeline-review", "the interaction ID")
	s.eq(at(d, "data", "participants", 0), "org_michael-scott-paper-company", "Jim's single affiliation brings his organization in")
	d = s.okIn(summary, "existing", pipeline...)
	s.eq(at(d, "data", "id"), pipelineID, "a retry with the Message-ID finds the same interaction")
	s.eq(at(s.ok("", "interaction", "list", "--limit", "0"), "total"), 1.0, "the retry duplicated nothing")

	followUp := []string{"task", "create", "--title", "Send Dwight the beet account terms", "--assignee", "us_pam",
		"--agent", "us_pam-agent", "--interaction", pipelineID, "--due", "2026-11-20"}
	d = s.ok("created", followUp...)
	taskID := str(at(d, "data", "id"))
	s.eq(at(d, "data", "purpose"), "follow-up", "the follow-up purpose")
	s.eq(at(d, "data", "from"), pipelineID, "the follow-up knows its interaction")
	s.eq(at(s.ok("existing", followUp...), "data", "id"), taskID, "the follow-up retried is the same task")
	s.eq(at(s.ok("", "task", "list", "--limit", "0"), "total"), 1.0, "one follow-up, not two")

	// An inbound interaction names who started it.
	d = s.okIn("Jan asked for the Scranton branch numbers.\n", "created", "interaction", "create", "--channel", "email",
		"--occurred-at", "2026-11-12T09:15:00Z", "--subject", "Branch numbers", "--participant", "pe_jan-levinson",
		"--direction", "inbound", "--initiated-by", "pe_jan-levinson", "--ref", "email:message:branch-numbers@example.com",
		"--summary-file", "-")
	branchID := str(at(d, "data", "id"))
	s.eq(at(d, "data", "participants", 0), "org_dunder-mifflin-corporate", "Jan's organization came with her")

	// --- Correcting mistakes -----------------------------------------------
	// Wrong initiator: Michael started it, not Pam. An update fixes it.
	d = s.ok("created", "interaction", "create", "--channel", "call", "--occurred-at", "2026-11-13T16:00:00Z",
		"--subject", "Call with Bob Vance", "--participant", "pe_bob-vance", "--initiated-by", "us_michael")
	callID := str(at(d, "data", "id"))
	s.eq(len(arr(at(s.ok("", "interaction", "list", "--initiated-by", "us_pam", "--limit", "0"), "data"))), 1,
		"before the fix Pam started one interaction")
	d = s.ok("updated", "interaction", "update", callID, "--initiated-by", "us_pam", "--fields", "initiated_by")
	s.eq(at(d, "data", "initiated_by"), "us_pam", "an update fixes the initiator")

	// Wrong actor: recorded as Michael, so it is deleted and created again.
	args := []string{"interaction", "create", "--channel", "meeting", "--occurred-at", "2026-11-16T11:00:00Z",
		"--subject", "Lunch at Chili's", "--participant", "pe_angela-martin", "--participant", "pe_dwight-schrute",
		"--initiated-by", "us_pam", "--summary-file", "-"}
	lunch := "Dwight ordered a crème brûlée for the whole table.\n"
	d = s.okIn(lunch, "created", append([]string{"--actor", "us_michael"}, args...)...)
	lunchID := str(at(d, "data", "id"))
	s.eq(at(s.ok("", "interaction", "get", lunchID, "--full"), "data", "created_by"), "us_michael", "the wrong actor is on the record")
	s.ok("deleted", "interaction", "delete", lunchID)
	d = s.okIn(lunch, "created", args...)
	s.eq(str(at(d, "data", "id")), lunchID, "the same ID comes back after the delete")
	s.eq(at(s.ok("", "interaction", "get", lunchID, "--full"), "data", "created_by"), "us_pam", "now Pam is the actor")
	// A delete of an interaction with a follow-up is refused, with its referrers.
	eo := s.fail("REFERENCED", 4, "interaction", "delete", pipelineID)
	s.eq(at(eo, "error", "details", "referrers", 0), taskID, "the refusal names the task")

	// --- A deal with several parties ---------------------------------------
	d = s.ok("created", "deal", "create", "--party", "org_michael-scott-paper-company", "--role", "seller",
		"--holder-role", "advisor", "--name", "Acquisition of Michael Scott Paper Company")
	dealID := str(at(d, "data", "id"))
	s.eq(dealID, "dl_michael-scott-paper-company", "the deal ID")
	s.eq(at(d, "data", "stage"), "prospect", "a deal starts at the first stage")
	s.ok("updated", "deal", "party", "add", dealID, "org_dunder-mifflin-corporate", "--role", "buyer")
	s.ok("updated", "deal", "party", "add", dealID, "pe_michael-scott", "--role", "client")
	s.ok("updated", "deal", "party", "add", dealID, "pe_jan-levinson", "--role", "counterparty")
	s.eq(at(s.ok("unchanged", "deal", "party", "add", dealID, "pe_jan-levinson", "--role", "counterparty"), "outcome"), "unchanged", "party add repeated")
	s.ok("updated", "deal", "party", "add", dealID, "pe_jan-levinson", "--role", "advisor")
	s.eq(len(arr(at(s.ok("", "deal", "get", dealID, "--full"), "data", "parties"))), 4, "four parties")

	s.ok("updated", "deal", "stage", dealID, "engaged")
	s.eq(at(s.ok("unchanged", "deal", "stage", dealID, "engaged"), "data", "stage"), "engaged", "same stage twice is unchanged")
	eo = s.fail("INVALID_VALUE", 2, "deal", "stage", dealID, "lost")
	s.eq(at(eo, "error", "details", "field"), "reason", "a lost stage asks for a reason")
	s.okIn("Angela thinks the price is too high.\n", "created", "note", "create", "--subject", dealID, "--subject", "pe_angela-martin", "--text-file", "-")

	// Work linked to the deal.
	d = s.okIn("Michael pitched the buyout in the conference room.\n", "created", "interaction", "create", "--channel", "meeting",
		"--occurred-at", "2026-11-17T15:00:00Z", "--subject", "Pitch to Corporate", "--participant", "pe_michael-scott",
		"--participant", "pe_jan-levinson", "--deal", dealID, "--initiated-by", "us_pam", "--summary-file", "-",
		"--ref", "calendar:event:pitch-corporate")
	pitchID := str(at(d, "data", "id"))
	d = s.ok("created", "task", "create", "--title", "Draft the term sheet", "--assignee", "us_michael",
		"--interaction", pitchID, "--due", "2026-11-25")
	s.eq(at(d, "data", "deal"), dealID, "a task from an interaction takes its single deal")
	s.eq(at(s.ok("", "task", "list", "--deal", dealID, "--limit", "0"), "total"), 1.0, "task list --deal finds it")
	s.eq(at(s.ok("", "interaction", "list", "--deal", dealID, "--limit", "0"), "total"), 1.0, "interaction list --deal finds it")

	// Lost, with a reason; hidden from the live reads; back to engaged.
	d = s.ok("updated", "deal", "stage", dealID, "lost", "--reason", "price")
	s.eq(at(d, "data", "lost_reason"), "price", "the reason is kept")
	s.eq(at(s.ok("", "deal", "list", "--limit", "0"), "total"), 0.0, "a lost deal is not live")
	d = s.ok("", "search", "acquisition")
	s.eq(len(arr(at(d, "data"))), 0, "search hides the lost deal")
	s.eq(at(d, "warnings", 0, "code"), "NOT_LIVE_HIDDEN", "and says so")
	s.eq(at(d, "warnings", 0, "details", "count"), 1.0, "how many it hid")
	d = s.ok("", "search", "acquisition", "--all")
	s.eq(at(d, "data", 0, "id"), dealID, "--all finds the lost deal")
	s.eq(at(d, "data", 0, "matches", 0, "field"), "name", "matched in the name")
	d = s.ok("updated", "deal", "stage", dealID, "engaged")
	s.eq(d["data"].(map[string]any)["lost_reason"], nil, "back from lost, the reason is cleared")
	s.eq(at(s.ok("", "deal", "list"), "total"), 1.0, "live again")
	s.ok("updated", "deal", "stage", dealID, "closing")

	// --- A project with tasks ----------------------------------------------
	d = s.ok("created", "project", "create", "--name", "Paper stock audit", "--deal", dealID)
	projectID := str(at(d, "data", "id"))
	for _, title := range []string{"Count the ream pallets", "Reconcile with the warehouse", "Report to Corporate"} {
		d = s.ok("created", "task", "create", "--title", title, "--assignee", "us_pam", "--project", projectID, "--due", "2026-12-01")
		s.eq(at(d, "data", "deal"), dealID, "a task in a project takes the project's deal")
	}
	s.ok("updated", "task", "status", str(at(s.ok("", "task", "list", "--project", projectID, "--limit", "0"), "data", 0, "id")), "done")
	s.fail("REFERENCED", 4, "project", "close", projectID)
	d = s.ok("updated", "project", "close", projectID, "--cancel-tasks")
	s.eq(len(arr(at(d, "data", "canceled_tasks"))), 2, "two live tasks canceled, the done one left alone")
	s.ok("unchanged", "project", "close", projectID, "--cancel-tasks")
	s.eq(at(s.ok("", "project", "list"), "total"), 0.0, "a closed project is not live")
	d = s.ok("", "search", "stock audit")
	s.eq(at(d, "warnings", 0, "code"), "NOT_LIVE_HIDDEN", "a closed project is a non-live hit too")
	s.eq(at(s.ok("", "search", "stock audit", "--all", "--type", "project"), "data", 0, "id"), projectID, "--all --type finds it")
	s.eq(at(s.ok("", "task", "list", "--project", projectID, "--all", "--category", "canceled", "--limit", "0"), "total"), 2.0, "canceled tasks")

	// --- Notes about several subjects ---------------------------------------
	d = s.okIn("Prince Family Paper is a rival for the Scranton accounts.\nWatch Bob Vance too.\n", "created", "note", "create",
		"--subject", "org_prince-family-paper", "--subject", "pe_bob-vance", "--subject", "org_vance-refrigeration", "--text-file", "-")
	noteID := str(at(d, "data", "id"))
	s.eq(at(d, "data", "excerpt"), "Prince Family Paper is a rival for the Scranton accounts.", "the excerpt is the first line")
	s.eq(at(s.ok("", "note", "list", "--related-to", "org_vance-refrigeration"), "total"), 1.0, "found by any subject")
	s.eq(at(s.ok("", "note", "list", "--related-to", "pe_bob-vance"), "data", 0, "id"), noteID, "found by the person")
	s.fail("INVALID_VALUE", 2, "note", "update", noteID, "--remove-subject", "pe_bob-vance", "--remove-subject", "org_vance-refrigeration", "--remove-subject", "org_prince-family-paper")

	// --- Briefings ---------------------------------------------------------
	d = s.ok("", "person", "get", "pe_jim-halpert", "--fields", "affiliations")
	s.eq(at(d, "data", "affiliations", 0, "title"), "Salesman", "briefing: Jim's title")
	d = s.ok("", "interaction", "list", "--participant", "pe_jim-halpert", "--fields", "summary", "--limit", "0")
	s.eq(at(d, "total"), 1.0, "briefing: Jim's interactions")
	s.eq(strings.HasPrefix(str(at(d, "data", 0, "summary")), "Jim and Dwight"), true, "briefing: the summary comes with --fields")
	s.eq(at(s.ok("", "note", "list", "--related-to", "pe_jim-halpert", "--limit", "0"), "total"), 0.0, "briefing: no note on Jim")
	d = s.ok("", "task", "list", "--related-to", "pe_jim-halpert", "--limit", "0", "--all")
	s.eq(at(d, "total"), 1.0, "briefing: the follow-up that came from an interaction with him")
	s.eq(at(d, "data", 0, "id"), taskID, "briefing: it is the follow-up")
	s.eq(at(s.ok("", "deal", "list", "--party", "pe_jim-halpert", "--limit", "0"), "total"), 0.0, "briefing: Jim has no deal")

	org := "org_michael-scott-paper-company"
	s.eq(at(s.ok("", "interaction", "list", "--participant", org, "--limit", "0"), "total"), 1.0, "org briefing: its interactions")
	s.eq(at(s.ok("", "deal", "list", "--party", org, "--limit", "0"), "data", 0, "id"), dealID, "org briefing: its deal")
	d = s.ok("", "task", "list", "--related-to", dealID, "--limit", "0")
	s.eq(at(d, "total"), 1.0, "org briefing: the deal's live tasks")
	s.eq(at(s.ok("", "task", "list", "--due-before", "2026-11-21", "--assignee", "us_pam"), "total"), 1.0, "due before a date excludes it")

	// --- Outreach: Pam in November ------------------------------------------
	s.okIn("Sent the October catalog.\n", "created", "interaction", "create", "--channel", "email", "--occurred-at", "2026-10-30T20:00:00Z",
		"--subject", "October catalog", "--participant", "pe_jim-halpert", "--initiated-by", "us_pam", "--summary-file", "-")
	s.okIn("Last day of the month.\n", "created", "interaction", "create", "--channel", "email", "--occurred-at", "2026-11-30T23:30:00-05:00",
		"--subject", "Month-end check-in", "--participant", "pe_david-wallace", "--initiated-by", "us_pam", "--summary-file", "-")
	s.okIn("The agent chased Bob.\n", "created", "interaction", "create", "--actor", "us_pam-agent", "--channel", "email",
		"--occurred-at", "2026-11-18T10:00:00Z", "--subject", "Chasing Bob", "--participant", "pe_bob-vance",
		"--initiated-by", "us_pam-agent", "--summary-file", "-")
	outreach := []string{"interaction", "list", "--direction", "outbound", "--initiated-by", "us_pam",
		"--from", "2026-11-01", "--to", "2026-11-30", "--limit", "0"}
	d = s.ok("", outreach...)
	// pipeline review, call with Bob, lunch, pitch, month-end; not October, not Jan's inbound, not the agent's.
	s.eq(at(d, "total"), 5.0, "outreach: Pam's November outbound interactions")
	for _, it := range arr(at(d, "data")) {
		if id := str(at(it, "id")); id == branchID || strings.Contains(id, "october") {
			t.Errorf("outreach counted %s", id)
		}
	}
	d = s.ok("", append(outreach, "--with-agents")...)
	s.eq(at(d, "total"), 6.0, "outreach: --with-agents adds the agent's")
	d = s.ok("", append(outreach, "--channel", "email")...)
	s.eq(at(d, "total"), 2.0, "outreach: one channel")
	seen := map[string]bool{}
	for _, it := range arr(at(s.ok("", outreach...), "data")) {
		for _, p := range arr(at(it, "participants")) {
			if strings.HasPrefix(str(p), "pe_") {
				seen[str(p)] = true
			}
		}
	}
	s.eq(len(seen), 7, "outreach: distinct people are counted from participants")

	// --- Search ------------------------------------------------------------
	d = s.ok("", "search", "beet", "--type", "interaction") // the follow-up title says beet too
	s.eq(at(d, "data", 0, "id"), pipelineID, "search: a word in a summary")
	s.eq(at(d, "data", 0, "matches", 0, "field"), "summary", "search: the field that matched")
	d = s.ok("", "search", "CREME brulee")
	s.eq(at(d, "data", 0, "id"), lunchID, "search: case and accents are ignored")
	s.eq(at(s.ok("", "search", "crème brûlée"), "data", 0, "id"), lunchID, "search: an accent in the query too")
	d = s.ok("", "search", "halpert")
	s.eq(at(d, "data", 0, "type"), "person", "search: a name")
	d = s.ok("", "search", "vance")
	s.eq(len(arr(at(d, "data"))), 4, "search: Bob, his company, the note and the call")
	d = s.ok("", "search", "vance", "--type", "person", "--type", "organization")
	s.eq(len(arr(at(d, "data"))), 2, "search: --type narrows")
	d = s.ok("", "search", "vance.example.com", "--type", "organization", "--fields", "domains")
	s.eq(at(d, "data", 0, "matches", 0, "field"), "domains", "search: a domain")
	s.eq(at(d, "data", 0, "domains", 0), "vance.example.com", "search: --fields widens the hit")
	s.eq(at(s.ok("", "search", "jim.halpert@example.com"), "data", 0, "id"), "pe_jim-halpert", "search: an email")
	s.eq(at(s.ok("", "search", "term sheet", "--type", "task"), "data", 0, "type"), "task", "search: a task by its title")
	s.eq(len(arr(at(s.ok("", "search", "vogel norte"), "data"))), 0, "search: nothing found is an empty list")
	s.fail("INVALID_VALUE", 2, "search", "   ")
	s.fail("INVALID_VALUE", 2, "search", "vance", "--type", "user")

	// --- The end -----------------------------------------------------------
	r := s.run("", "check")
	if r.Code != 0 {
		t.Fatalf("check: code %d, stdout %s, stderr %s", r.Code, r.Stdout, r.Stderr)
	}
	s.eq(len(arr(at(r.JSON(t), "data"))), 0, "check finds nothing")
}

type scenario struct {
	t   *testing.T
	e   *env
	bin string
	n   int
}

func (s *scenario) run(stdin string, args ...string) result {
	s.t.Helper()
	s.n++
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(s.bin, args...)
	cmd.Dir = s.e.Workspace
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			s.t.Fatalf("%v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	s.t.Logf("step %02d  kravis %s", s.n, strings.Join(args, " "))
	return result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

// ok runs a command that must succeed and, when outcome is not empty, report it.
func (s *scenario) ok(outcome string, args ...string) map[string]any {
	s.t.Helper()
	return s.okIn("", outcome, args...)
}

func (s *scenario) okIn(stdin, outcome string, args ...string) map[string]any {
	s.t.Helper()
	r := s.run(stdin, args...)
	if r.Code != 0 || r.Stderr != "" {
		s.t.Fatalf("%v: code %d, stdout %s, stderr %s", args, r.Code, r.Stdout, r.Stderr)
	}
	out := r.JSON(s.t)
	if outcome != "" && out["outcome"] != outcome {
		s.t.Fatalf("%v: outcome %v, want %s (%s)", args, out["outcome"], outcome, r.Stdout)
	}
	return out
}

func (s *scenario) fail(code string, exit int, args ...string) map[string]any {
	s.t.Helper()
	r := s.run("", args...)
	if r.Code != exit || r.Stdout != "" {
		s.t.Fatalf("%v: code %d, want %d (stdout %s, stderr %s)", args, r.Code, exit, r.Stdout, r.Stderr)
	}
	eo := r.ErrorJSON(s.t)
	if got := at(eo, "error", "code"); got != code {
		s.t.Fatalf("%v: error %v, want %s", args, got, code)
	}
	return eo
}

func (s *scenario) eq(got, want any, what string) {
	s.t.Helper()
	if got != want {
		s.t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

// at walks a decoded JSON value by object keys and array indexes; nil when
// the path does not exist.
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func arr(v any) []any { a, _ := v.([]any); return a }

func str(v any) string { s, _ := v.(string); return s }
