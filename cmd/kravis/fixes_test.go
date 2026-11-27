package main

import (
	"slices"
	"strings"
	"testing"
)

func warningsOf(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	var ws []map[string]any
	if raw, ok := out["warnings"].([]any); ok {
		for _, w := range raw {
			ws = append(ws, w.(map[string]any))
		}
	}
	return ws
}

func TestSearchWarnsOfHitsThatAreNotLive(t *testing.T) {
	e := searchWorld(t)
	e.ok("organization", "create", "--name", "Finch Retired")
	e.ok("organization", "update", "org_finch-retired", "--live=false")
	e.ok("organization", "create", "--name", "Finch Gone")
	e.ok("organization", "update", "org_finch-gone", "--live=false")
	e.ok("organization", "create", "--name", "Finch Active")

	ws := warningsOf(t, e.ok("search", "finch"))
	if len(ws) != 1 || ws[0]["code"] != "NOT_LIVE_HIDDEN" || ws[0]["details"].(map[string]any)["count"] != float64(2) ||
		!strings.Contains(ws[0]["message"].(string), "--all") {
		t.Errorf("warnings %v", ws)
	}
	if ws := warningsOf(t, e.ok("search", "finch", "--all")); len(ws) != 0 {
		t.Errorf("--all warns: %v", ws)
	}
	if ws := warningsOf(t, e.ok("search", "elena")); len(ws) != 0 {
		t.Errorf("nothing hidden, yet: %v", ws)
	}
	// A hit that matches only a word of the text is not counted: every word must match.
	if ws := warningsOf(t, e.ok("search", "finch retired zzz")); len(ws) != 0 {
		t.Errorf("a partial match counted: %v", ws)
	}
}

func TestSearchFieldsAndFullWidenTheHits(t *testing.T) {
	e := searchWorld(t)
	e.ok(mkNote(t, "Finch likes tea.\n", elenaID)...)
	e.ok(record("Finch call", "--summary-file", textFile(t, "Finch asked for the data room.\n"))...)

	plain := hitsOf(t, e.ok("search", "finch"))
	for _, h := range plain {
		if _, ok := h["summary"]; ok {
			t.Errorf("compact hit has a summary: %v", h)
		}
	}
	// A name that only some types define widens only their hits.
	wide := hitsOf(t, e.ok("search", "finch", "--fields", "summary,text"))
	seen := map[string]bool{}
	for _, h := range wide {
		switch h["type"] {
		case "interaction":
			seen["summary"] = h["summary"] == "Finch asked for the data room.\n" || h["summary"] == "Finch asked for the data room."
		case "note":
			seen["text"] = strings.HasPrefix(h["text"].(string), "Finch likes tea")
		}
	}
	if !seen["summary"] || !seen["text"] {
		t.Errorf("wide hits %v (%v)", wide, seen)
	}
	for _, h := range hitsOf(t, e.ok("search", "elena", "--full")) {
		if h["type"] == "person" {
			if _, ok := h["created_at"]; !ok {
				t.Errorf("--full hit: %v", h)
			}
			if _, ok := h["matches"]; !ok || h["updated_at"] == nil {
				t.Errorf("--full hit lost its search fields: %v", h)
			}
		}
	}
	eo := e.refused("UNKNOWN_FIELD", 2, "search", "finch", "--fields", "nonsense")
	if details(t, eo)["field"] != "nonsense" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("UNKNOWN_FIELD", 2, "search", "elena", "--type", "person", "--fields", "summary")
}

func TestARepeatedTaskCreateShowsTheFieldsOfTheCreatedResponse(t *testing.T) {
	e := taskWorld(t)
	e.ok(record("Data room documentation")...)
	args := mkTask("Send the teaser", "--interaction", dataRoomID, "--description-file", textFile(t, "Send it today.\n"))
	made := data(t, e.ok(args...))
	again := data(t, e.ok(args...))
	if data(t, e.ok(args...))["id"] != made["id"] || again["from"] != dataRoomID || again["purpose"] != "follow-up" {
		t.Fatalf("existing: %v", again)
	}
	keys := func(m map[string]any) []string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		slices.Sort(ks)
		return ks
	}
	if !slices.Equal(keys(made), keys(again)) {
		t.Errorf("created %v, existing %v", keys(made), keys(again))
	}
	if again["description"] != made["description"] {
		t.Errorf("description %v, %v", made["description"], again["description"])
	}
}

func TestTheUserViewShowsItsPersonWhenLinked(t *testing.T) {
	e := newEnv(t)
	r := e.init("--user-email", userEmail)
	if r.Code != 0 {
		t.Fatalf("code %d, %q", r.Code, r.Stderr)
	}
	if u := obj(t, data(t, r.JSON(t)), "user"); u["person"] != "pe_ana-example" {
		t.Errorf("init user %v", u)
	}
	e.ok("user", "create", "eva-acme", "--human", "--holder", orgID, "--person", "pe_ana-example")
	if u := data(t, e.ok("user", "get", "us_eva-acme")); u["person"] != "pe_ana-example" {
		t.Errorf("user get %v", u)
	}
	e.ok("user", "create", "bob-acme", "--human", "--holder", orgID)
	if u := data(t, e.ok("user", "get", "us_bob-acme")); u["person"] != nil {
		t.Errorf("an unlinked user shows a person: %v", u)
	}
}

func TestWorkspaceExistsNamesTheWorkspacesValueInDetails(t *testing.T) {
	e := newEnv(t)
	if r := e.init("--user-email", userEmail); r.Code != 0 {
		t.Fatalf("code %d, %q", r.Code, r.Stderr)
	}
	eo := wantError(t, e.init("--user-email", "other@example.com"), "WORKSPACE_EXISTS", 2)
	if d := details(t, eo); d["field"] != "user-email" || d["value"] != "other@example.com" || d["workspace"] != userEmail {
		t.Errorf("email details %v", d)
	}
	eo = wantError(t, e.run("init", "--timezone", "UTC", "--organization", orgName, "--user", userKey, "--user-name", userName),
		"WORKSPACE_EXISTS", 2)
	if d := details(t, eo); d["field"] != "timezone" || d["value"] != "UTC" || d["workspace"] != tz {
		t.Errorf("timezone details %v", d)
	}
	eo = wantError(t, e.run("init", "--timezone", tz, "--organization", "Other Example", "--user", userKey, "--user-name", userName),
		"WORKSPACE_EXISTS", 2)
	if d := details(t, eo); d["field"] != "organization" || d["workspace"] != orgName {
		t.Errorf("organization details %v", d)
	}
	eo = wantError(t, e.run("init", "--timezone", tz, "--organization", orgName, "--user", userKey, "--user-name", "Zed"),
		"WORKSPACE_EXISTS", 2)
	if d := details(t, eo); d["field"] != "user-name" || d["workspace"] != userName {
		t.Errorf("name details %v", d)
	}
}

func TestHelpSaysWhereTheActorIsStoredAndThatEveryWordMustBeInOneRecord(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		cmd  string
		want string
	}{
		{"init", "$XDG_CONFIG_HOME/kravis/config.json"},
		{"whoami", "$XDG_CONFIG_HOME/kravis/config.json"},
		{"search", "Every word must appear in the same record"},
	} {
		if r := e.run(c.cmd, "--help"); !strings.Contains(strings.Join(strings.Fields(r.Stdout), " "), c.want) {
			t.Errorf("%s --help lacks %q: %s", c.cmd, c.want, r.Stdout)
		}
	}
}

func TestTheSkillTeachesTheLinesOfTheThirdRun(t *testing.T) {
	skill := skillFiles(t)["SKILL.md"]
	for _, want := range []string{
		"`--agent <itself>`", "--role client --role seller --holder-role advisor",
		"start with `kravis person get", "`kravis task list --related-to <deal> --limit 0`", "finds nothing when the words are in a person and an organization",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("the skill lacks %q", want)
		}
	}
}
