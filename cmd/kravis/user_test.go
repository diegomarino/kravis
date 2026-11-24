package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

const (
	agentID  = "us_mail-acme"
	otherOrg = "org_other-example"
)

// mustAgent creates an agent under the first human.
func (e *env) mustAgent() map[string]any {
	e.t.Helper()
	return e.ok("user", "create", "mail-acme", "--agent", "--holder", orgID, "--handler", userID)
}

func TestIDENTITY_INV_01_AUserNeverChangesHolder(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("organization", "create", "--name", "Other Example", "--own")
	d := outcomeOf(t, e.ok("user", "create", "bea-other", "--human", "--holder", otherOrg, "--fields", "holder"), "created")
	if d["holder"] != otherOrg || d["kind"] != "human" {
		t.Errorf("created: %v", d)
	}
	eo := e.refused("INVALID_VALUE", 2, "user", "update", "us_bea-other", "--holder", orgID)
	if obj(t, eo, "details")["field"] != "holder" {
		t.Errorf("details %v", eo["details"])
	}
	// The same holder again changes nothing.
	outcomeOf(t, e.ok("user", "update", "us_bea-other", "--holder", otherOrg), "unchanged")
	// A holder must exist and be an own organization.
	e.ok("organization", "create", "--name", "Plain Example")
	e.refused("INVALID_VALUE", 2, "user", "create", "cy", "--human", "--holder", "org_plain-example")
	e.refused("NOT_FOUND", 3, "user", "create", "cy", "--human", "--holder", "org_nowhere")
}

func TestIDENTITY_INV_02_AUserNeverChangesKind(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.mustAgent()
	e.refused("INVALID_VALUE", 2, "user", "update", agentID, "--human")
	e.refused("INVALID_VALUE", 2, "user", "update", userID, "--agent")
	outcomeOf(t, e.ok("user", "update", agentID, "--agent"), "unchanged")
	e.refused("INVALID_VALUE", 2, "user", "create", "both", "--human", "--agent", "--holder", orgID)
	eo := e.refused("INVALID_VALUE", 2, "user", "create", "neither", "--holder", orgID)
	if obj(t, eo, "details")["field"] != "kind" {
		t.Errorf("details %v", eo["details"])
	}
}

func TestIDENTITY_INV_03_AnAgentHasAHandlerThatIsALiveHumanOfItsHolder(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("organization", "create", "--name", "Other Example", "--own")
	e.ok("user", "create", "bea-other", "--human", "--holder", otherOrg)

	d := outcomeOf(t, e.mustAgent(), "created")
	if d["id"] != agentID || d["kind"] != "agent" || d["holder"] != orgID {
		t.Errorf("agent: %v", d)
	}
	if h := data(t, e.ok("user", "get", agentID, "--fields", "handler"))["handler"]; h != userID {
		t.Errorf("handler %v", h)
	}

	e.refused("INVALID_VALUE", 2, "user", "create", "a1", "--agent", "--holder", orgID)                       // no handler
	e.refused("INVALID_VALUE", 2, "user", "create", "h1", "--human", "--holder", orgID, "--handler", userID)  // a human has none
	e.refused("INVALID_VALUE", 2, "user", "create", "a2", "--agent", "--holder", orgID, "--handler", agentID) // an agent
	e.refused("NOT_FOUND", 3, "user", "create", "a3", "--agent", "--holder", orgID, "--handler", "us_nobody") // missing
	eo := e.refused("HOLDER_MISMATCH", 2, "user", "create", "a4", "--agent", "--holder", orgID, "--handler", "us_bea-other")
	if obj(t, eo, "details")["field"] != "handler" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("INVALID_VALUE", 2, "user", "update", agentID, "--handler", "us_bea-other")
	outcomeOf(t, e.ok("user", "update", agentID, "--handler", userID), "unchanged")

	// A handler that is not live cannot take an agent.
	e.ok("user", "create", "dan-acme", "--human", "--holder", orgID)
	e.ok("user", "update", "us_dan-acme", "--live=false")
	e.refused("INVALID_VALUE", 2, "user", "create", "a5", "--agent", "--holder", orgID, "--handler", "us_dan-acme")

	// Deactivating the handler of a live agent is refused, with the agent named.
	eo = e.refused("REFERENCED", 4, "user", "update", userID, "--live=false")
	if refs := obj(t, eo, "details")["referrers"]; fmt.Sprint(refs) != "["+agentID+"]" {
		t.Errorf("referrers %v", refs)
	}
	// Agent first, then the human; the deactivated agent cannot act.
	outcomeOf(t, e.ok("user", "update", agentID, "--live=false"), "updated")
	e.refused("ACTOR_NOT_LIVE", 2, "--actor", agentID, "organization", "create", "--name", "Never Example")
	// With the agent off, its handler may go too.
	outcomeOf(t, e.ok("user", "update", userID, "--live=false"), "updated")
}

func TestWORKSPACE_AC_10_DeletingAReferencedRecordFailsAndListsTheReferrers(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.mustAgent()
	e.ok("organization", "create", "--name", "Grupo Sierra")
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	e.ok("person", "affiliation", "add", elenaID, "org_grupo-sierra", "--title", "CFO")

	referrers := func(eo map[string]any) string { return fmt.Sprint(obj(t, eo, "details")["referrers"]) }
	// A human who is a handler.
	if got := referrers(e.refused("REFERENCED", 4, "user", "delete", userID)); got != "["+agentID+"]" {
		t.Errorf("handler referrers %s", got)
	}
	// An organization that is a holder.
	if got := referrers(e.refused("REFERENCED", 4, "organization", "delete", orgID)); got != "["+userID+" "+agentID+"]" {
		t.Errorf("holder referrers %s", got)
	}
	// An organization with affiliations.
	if got := referrers(e.refused("REFERENCED", 4, "organization", "delete", "org_grupo-sierra")); got != "["+elenaID+"]" {
		t.Errorf("affiliation referrers %s", got)
	}
	// A person linked to a human.
	e.ok("user", "create", "eva-acme", "--human", "--holder", orgID, "--person", elenaEmail)
	if got := referrers(e.refused("REFERENCED", 4, "person", "delete", elenaID)); got != "[us_eva-acme]" {
		t.Errorf("person referrers %s", got)
	}

	// Once nothing refers to them, they go.
	outcomeOf(t, e.ok("user", "delete", "us_eva-acme"), "deleted")
	outcomeOf(t, e.ok("person", "affiliation", "remove", elenaID, "org_grupo-sierra"), "deleted")
	outcomeOf(t, e.ok("organization", "delete", "org_grupo-sierra"), "deleted")
	outcomeOf(t, e.ok("user", "delete", agentID), "deleted")
	e.ok("user", "update", userID, "--name", "Ana Example") // still there
	e.refused("NOT_FOUND", 3, "user", "delete", agentID)
}

func TestWORKSPACE_INV_04_TheOwnMarkStaysWhileTheOrganizationIsAHolder(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	eo := e.refused("REFERENCED", 4, "organization", "update", orgID, "--own=false")
	if refs := obj(t, eo, "details")["referrers"]; fmt.Sprint(refs) != "["+userID+"]" {
		t.Errorf("referrers %v", refs)
	}
	// An affiliation is not a holder: it does not keep the mark.
	e.ok("organization", "create", "--name", "Spare Example", "--own")
	e.ok("person", "create", "--name", "Elena Ruiz")
	e.ok("person", "affiliation", "add", elenaID, "org_spare-example")
	outcomeOf(t, e.ok("organization", "update", "org_spare-example", "--own=false"), "updated")
	// A holder that is a user's holder only: still refused.
	e.ok("organization", "create", "--name", "Other Example", "--own")
	e.ok("user", "create", "bea-other", "--human", "--holder", otherOrg)
	e.refused("REFERENCED", 4, "organization", "update", otherOrg, "--own=false")
	outcomeOf(t, e.ok("organization", "update", otherOrg, "--own"), "unchanged")
}

func TestDIRECTORY_INV_04_OneAffiliationPerPairWithAFreeTitle(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("organization", "create", "--name", "Grupo Sierra")
	e.ok("organization", "create", "--name", "Fundacion Vega")
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)

	d := outcomeOf(t, e.ok("person", "affiliation", "add", elenaID, "org_grupo-sierra", "--title", "CFO"), "created")
	if fmt.Sprint(d["affiliations"]) != "[map[organization:org_grupo-sierra title:CFO]]" {
		t.Errorf("created: %v", d)
	}
	d = outcomeOf(t, e.ok("person", "affiliation", "add", elenaID, "org_grupo-sierra", "--title", "Board member"), "updated")
	if fmt.Sprint(d["affiliations"]) != "[map[organization:org_grupo-sierra title:Board member]]" {
		t.Errorf("updated: %v", d)
	}
	before := e.Files()
	outcomeOf(t, e.ok("person", "affiliation", "add", elenaID, "org_grupo-sierra", "--title", "Board member"), "unchanged")
	// Without --title the title stays; the email is an alias of the person.
	outcomeOf(t, e.ok("person", "affiliation", "add", elenaEmail, "org_grupo-sierra"), "unchanged")
	if !maps.Equal(before, e.Files()) {
		t.Error("unchanged wrote")
	}
	// Several organizations; the list is sorted by organization.
	e.ok("person", "affiliation", "add", elenaID, "org_fundacion-vega")
	d = data(t, e.ok("person", "get", elenaID, "--fields", "affiliations"))
	if fmt.Sprint(d["affiliations"]) != "[map[organization:org_fundacion-vega] map[organization:org_grupo-sierra title:Board member]]" {
		t.Errorf("two affiliations: %v", d)
	}
	// An empty title clears it.
	d = outcomeOf(t, e.ok("person", "affiliation", "add", elenaID, "org_grupo-sierra", "--title", ""), "updated")
	if strings.Contains(fmt.Sprint(d["affiliations"]), "title") {
		t.Errorf("cleared: %v", d)
	}

	e.refused("NOT_FOUND", 3, "person", "affiliation", "add", elenaID, "org_nowhere")
	e.refused("NOT_FOUND", 3, "person", "affiliation", "add", "pe_nobody", "org_grupo-sierra")
	e.refused("NOT_FOUND", 3, "person", "affiliation", "remove", elenaID, "org_nowhere")

	outcomeOf(t, e.ok("person", "affiliation", "remove", elenaID, "org_grupo-sierra"), "deleted")
	e.refused("NOT_FOUND", 3, "person", "affiliation", "remove", elenaID, "org_grupo-sierra")
	if d := data(t, e.ok("person", "get", elenaID, "--fields", "affiliations")); fmt.Sprint(d["affiliations"]) != "[map[organization:org_fundacion-vega]]" {
		t.Errorf("after remove: %v", d)
	}
}

func TestIDENTITY_AC_01_AWriteWithoutAnActorFailsAndWhoamiShowsTheActor(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.mustAgent()

	// Without --actor, KRAVIS_ACTOR or a stored default: exit 2 and a hint naming both.
	must(t, os.Remove(e.localConfig()))
	for _, args := range [][]string{
		{"user", "create", "x", "--human", "--holder", orgID},
		{"person", "create", "--name", "Elena Ruiz"},
		{"whoami"},
	} {
		eo := e.refused("MISSING_ACTOR", 2, args...)
		hint, _ := eo["hint"].(string)
		if !strings.Contains(hint, "--actor") || !strings.Contains(hint, "KRAVIS_ACTOR") {
			t.Errorf("%v: hint %q", args, hint)
		}
	}
	e.ok("user", "list") // reads need none

	// whoami with the agent, from the flag and from the environment.
	d := data(t, e.ok("--actor", agentID, "whoami"))
	if d["actor"] != agentID || d["source"] != "flag" || d["holder"] != orgID {
		t.Errorf("whoami: %v", d)
	}
	t.Setenv("KRAVIS_ACTOR", agentID)
	if d := data(t, e.ok("whoami")); d["source"] != "environment" || d["actor"] != agentID {
		t.Errorf("whoami: %v", d)
	}
	// An agent can act, and is the creator of what it writes.
	d = data(t, e.ok("person", "create", "--name", "Elena Ruiz", "--fields", "created_by"))
	if d["created_by"] != agentID {
		t.Errorf("created_by %v", d["created_by"])
	}
	// A deactivated agent is refused as actor.
	t.Setenv("KRAVIS_ACTOR", userID)
	e.ok("user", "update", agentID, "--live=false")
	t.Setenv("KRAVIS_ACTOR", agentID)
	e.refused("ACTOR_NOT_LIVE", 2, "whoami")
}

func TestUserFileAsStoredAndFieldOrder(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.mustAgent()
	raw, err := os.ReadFile("records/users/" + agentID + ".json")
	must(t, err)
	want := `{
  "id": "us_mail-acme",
  "holder": "org_acme-example",
  "kind": "agent",
  "handler": "us_ana-acme",
  "live": true,
  "version": 1,
  "created_at": "2026-11-24T09:30:00Z",
  "created_by": "us_ana-acme",
  "updated_at": "2026-11-24T09:30:00Z",
  "updated_by": "us_ana-acme"
}
`
	if string(raw) != want {
		t.Errorf("stored:\n%s", raw)
	}
}

func TestUserCreateIsIdempotentAndConflictsOnOtherData(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("organization", "create", "--name", "Other Example", "--own")
	e.ok("user", "create", "bea-other", "--human", "--holder", otherOrg)
	e.mustAgent()
	before := e.Files()
	outcomeOf(t, e.ok("user", "create", "mail-acme", "--agent", "--holder", orgID, "--handler", userID), "existing")
	if !maps.Equal(before, e.Files()) {
		t.Error("existing wrote")
	}
	for field, args := range map[string][]string{
		"holder": {"user", "create", "mail-acme", "--agent", "--holder", otherOrg, "--handler", "us_bea-other"},
		"kind":   {"user", "create", "mail-acme", "--human", "--holder", orgID},
		"name":   {"user", "create", "mail-acme", "--agent", "--holder", orgID, "--handler", userID, "--name", "Mail"},
	} {
		eo := e.refused("DEDUP_CONFLICT", 4, args...)
		if obj(t, eo, "details")["field"] != field {
			t.Errorf("%s: details %v", field, eo["details"])
		}
	}
	e.refused("INVALID_VALUE", 2, "user", "create", "Bad Key", "--human", "--holder", orgID)
	e.refused("UNKNOWN_FIELD", 2, "user", "create", "x", "--human", "--holder", orgID, "--fields", "nope")
}

func TestUserPersonLink(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	e.mustAgent()
	e.refused("INVALID_VALUE", 2, "user", "create", "a9", "--agent", "--holder", orgID, "--handler", userID, "--person", elenaID)
	e.refused("NOT_FOUND", 3, "user", "create", "h9", "--human", "--holder", orgID, "--person", "pe_nobody")
	e.refused("NOT_FOUND", 3, "user", "update", userID, "--person", "nobody@example.com")
	e.refused("INVALID_VALUE", 2, "user", "update", agentID, "--person", elenaID)
	d := outcomeOf(t, e.ok("user", "update", userID, "--person", elenaEmail, "--fields", "person"), "updated")
	if d["person"] != elenaID {
		t.Errorf("person %v", d)
	}
	d = outcomeOf(t, e.ok("user", "update", userID, "--person", ""), "updated")
	if _, has := d["person"]; has {
		t.Errorf("unlinked: %v", d)
	}
}

func TestReactivatingAnAgentNeedsALiveHandler(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("user", "create", "dan-acme", "--human", "--holder", orgID)
	e.ok("user", "create", "bot-acme", "--agent", "--holder", orgID, "--handler", "us_dan-acme")
	e.ok("user", "update", "us_bot-acme", "--live=false")
	e.ok("user", "update", "us_dan-acme", "--live=false")
	e.refused("INVALID_VALUE", 2, "user", "update", "us_bot-acme", "--live")
	e.ok("user", "update", "us_dan-acme", "--live")
	outcomeOf(t, e.ok("user", "update", "us_bot-acme", "--live"), "updated")
}

func TestCheckReportsBrokenUsersAndAffiliations(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("organization", "create", "--name", "Other Example", "--own")
	e.ok("organization", "create", "--name", "Plain Example")
	e.ok("user", "create", "bea-other", "--human", "--holder", otherOrg)
	e.ok("person", "create", "--name", "Elena Ruiz")
	e.ok("person", "affiliation", "add", elenaID, "org_plain-example")
	e.mustAgent()
	if r := e.run("check"); r.Code != 0 {
		t.Fatalf("clean: %s", r.Stdout)
	}

	write := func(path, body string) { must(t, os.WriteFile(path, []byte(body), 0o600)) }
	write("records/users/us_nokind.json", `{"id":"us_nokind","holder":"org_acme-example","kind":"robot","live":true}`)
	write("records/users/us_nohandler.json", `{"id":"us_nohandler","holder":"org_acme-example","kind":"agent","live":true}`)
	write("records/users/us_handled.json", `{"id":"us_handled","holder":"org_acme-example","kind":"human","handler":"us_ana-acme","live":true}`)
	write("records/users/us_ghost.json", `{"id":"us_ghost","holder":"org_nowhere","kind":"human","person":"pe_nobody","live":true}`)
	write("records/users/us_plain.json", `{"id":"us_plain","holder":"org_plain-example","kind":"human","live":true}`)
	write("records/users/us_lost.json", `{"id":"us_lost","holder":"org_acme-example","kind":"agent","handler":"us_gone","live":true}`)
	write("records/users/us_ofagent.json", `{"id":"us_ofagent","holder":"org_acme-example","kind":"agent","handler":"us_mail-acme","live":true}`)
	write("records/users/us_foreign.json", `{"id":"us_foreign","holder":"org_acme-example","kind":"agent","handler":"us_bea-other","live":true}`)
	write("records/users/us_nolive.json", `{"id":"us_nolive","holder":"org_acme-example","kind":"human"}`)
	write("records/people/pe_twice.json", `{"id":"pe_twice","name":"Twice","affiliations":[{"organization":"org_plain-example"},{"organization":"org_plain-example","title":"x"}],"live":true}`)
	write("records/people/pe_dangling.json", `{"id":"pe_dangling","name":"Dangling","affiliations":[{"organization":"org_nowhere"},{"organization":"Bad Id"}],"live":true}`)
	// A deactivated handler with a live agent, by hand.
	raw, err := os.ReadFile("records/users/" + userID + ".json")
	must(t, err)
	must(t, os.WriteFile("records/users/us_off.json", []byte(strings.ReplaceAll(strings.ReplaceAll(string(raw), userID, "us_off"), `"live": true`, `"live": false`)), 0o600))
	write("records/users/us_orphaned.json", `{"id":"us_orphaned","holder":"org_acme-example","kind":"agent","handler":"us_off","live":true}`)

	before := e.Files()
	r := e.run("check")
	if r.Code != 6 {
		t.Fatalf("code %d: %s %s", r.Code, r.Stdout, r.Stderr)
	}
	var got []string
	for _, f := range r.JSON(t)["data"].([]any) {
		m := f.(map[string]any)
		got = append(got, m["code"].(string)+" "+m["record"].(string))
	}
	slices.Sort(got)
	want := []string{
		"BROKEN_REFERENCE records/people/pe_dangling.json",
		"BROKEN_REFERENCE records/users/us_ghost.json",
		"BROKEN_REFERENCE records/users/us_ghost.json",
		"BROKEN_REFERENCE records/users/us_lost.json",
		"INVALID_RECORD records/people/pe_dangling.json",
		"INVALID_RECORD records/people/pe_twice.json",
		"INVALID_RECORD records/users/us_foreign.json",
		"INVALID_RECORD records/users/us_handled.json",
		"INVALID_RECORD records/users/us_nohandler.json",
		"INVALID_RECORD records/users/us_nokind.json",
		"INVALID_RECORD records/users/us_nolive.json",
		"INVALID_RECORD records/users/us_ofagent.json",
		"INVALID_RECORD records/users/us_orphaned.json",
		"INVALID_RECORD records/users/us_plain.json",
	}
	if !slices.Equal(got, want) {
		t.Errorf("findings\ngot  %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
	if !maps.Equal(before, e.Files()) {
		t.Error("check wrote")
	}
}
