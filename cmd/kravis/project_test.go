package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

const projectFile = "records/projects/pr_data-room.json"

// projectWorld is a taskWorld with holder A's deal dl_grupo-sierra and
// holder B's deal dl_elena-ruiz.
func projectWorld(t *testing.T) *env {
	t.Helper()
	e := taskWorld(t)
	e.ok(mkDeal()...)
	e.ok(as(otherAgent, "deal", "create", "--party", elenaID, "--holder-role", "buyer")...)
	return e
}

const otherDeal = "dl_elena-ruiz"

func mkProject(extra ...string) []string {
	return slices.Concat([]string{"project", "create", "--name", "Data room"}, extra)
}

func TestWORK_INV_02_AProjectIsTiedToAtMostOneDealOfItsHolder(t *testing.T) {
	e := projectWorld(t)
	d := outcomeOf(t, e.ok(mkProject("--deal", sierraDeal)...), "created")
	if d["id"] != "pr_data-room" || d["deal"] != sierraDeal || d["live"] != true || d["holder"] != orgID {
		t.Errorf("created %v", d)
	}
	if rec := readRecord(t, projectFile); rec["deal"] != sierraDeal || rec["live"] != true {
		t.Errorf("stored %v", rec)
	}
	// A deal may have many projects; the same name gets -2 and a warning.
	out := e.ok("project", "create", "--name", "Data room", "--deal", sierraDeal)
	if data(t, out)["id"] != "pr_data-room-2" || !slices.Contains(warningCodes(out), "SIMILAR_EXISTS") {
		t.Errorf("second %v", out)
	}
	if got := ids(t, e.ok("project", "list", "--deal", sierraDeal)); fmt.Sprint(got) != "[pr_data-room pr_data-room-2]" {
		t.Errorf("by deal %v", got)
	}
	// Another holder's deal is refused, and so is one that does not exist.
	eo := e.refused("HOLDER_MISMATCH", 2, mkProject("--name", "Other", "--deal", otherDeal)...)
	if details(t, eo)["field"] != "deal" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("HOLDER_MISMATCH", 2, "project", "update", "pr_data-room", "--deal", otherDeal)
	e.refused("NOT_FOUND", 3, mkProject("--name", "Ghost", "--deal", "dl_ghost")...)
	// The deal changes at any time, and clears with "".
	outcomeOf(t, e.ok("project", "update", "pr_data-room-2", "--deal", ""), "updated")
	if _, has := readRecord(t, "records/projects/pr_data-room-2.json")["deal"]; has {
		t.Error("the deal was not cleared")
	}
	outcomeOf(t, e.ok("project", "update", "pr_data-room-2", "--deal", sierraDeal), "updated")
	outcomeOf(t, e.ok("project", "update", "pr_data-room-2", "--deal", sierraDeal), "unchanged")
}

func TestWORK_INV_01_ATaskHasOneParentAndTakesTheDealOfItsProject(t *testing.T) {
	e := projectWorld(t)
	e.ok(mkProject("--deal", sierraDeal)...)
	d := outcomeOf(t, e.ok(mkTask("In the project", "--project", "pr_data-room")...), "created")
	if d["project"] != "pr_data-room" || d["deal"] != sierraDeal {
		t.Errorf("created %v", d)
	}
	id := d["id"].(string)
	rec := readRecord(t, taskFile(t, id))
	if rec["project"] != "pr_data-room" {
		t.Errorf("stored %v", rec)
	}
	if _, has := rec["deal"]; has {
		t.Errorf("a task in a project stores no deal: %v", rec)
	}
	// get and list show the deal the task takes from its project.
	if g := data(t, e.ok("task", "get", id)); g["deal"] != sierraDeal {
		t.Errorf("get %v", g)
	}
	// A task in a deal stores the deal.
	dd := data(t, e.ok(mkTask("In the deal", "--deal", sierraDeal)...))
	if readRecord(t, taskFile(t, dd["id"].(string)))["deal"] != sierraDeal || dd["deal"] != sierraDeal {
		t.Errorf("deal task %v", dd)
	}
	e.ok(mkTask("Free")...)
	// Both parents are refused, and so are other holders' and unknown ones.
	e.refused("INVALID_VALUE", 2, mkTask("Both", "--project", "pr_data-room", "--deal", sierraDeal)...)
	e.refused("HOLDER_MISMATCH", 2, mkTask("Other deal", "--deal", otherDeal)...)
	e.ok(as(otherAgent, "project", "create", "--name", "Their project")...)
	eo := e.refused("HOLDER_MISMATCH", 2, mkTask("Other project", "--project", "pr_their-project")...)
	if details(t, eo)["field"] != "project" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("NOT_FOUND", 3, mkTask("Ghost", "--project", "pr_ghost")...)
	// The parent is fixed at creation.
	eo = e.refused("UNKNOWN_FIELD", 2, "task", "update", id, "--project", "pr_data-room")
	if !strings.Contains(fmt.Sprint(eo["hint"]), "delete the task") {
		t.Errorf("hint %v", eo["hint"])
	}
	e.refused("UNKNOWN_FIELD", 2, "task", "update", id, "--deal", sierraDeal)
	// Lists filter by parent: a deal matches its projects' tasks too.
	if got := ids(t, e.ok("task", "list", "--project", "pr_data-room")); len(got) != 1 || got[0] != id {
		t.Errorf("by project %v", got)
	}
	if got := ids(t, e.ok("task", "list", "--deal", sierraDeal)); len(got) != 2 {
		t.Errorf("by deal %v", got)
	}
}

func TestWORK_AC_01_ClosingAProjectWithLiveTasksFailsAndListsThem(t *testing.T) {
	e := projectWorld(t)
	e.ok(mkProject("--deal", sierraDeal)...)
	id := idOf(t, e.ok(mkTask("Send the teaser", "--project", "pr_data-room")...))
	eo := e.refused("REFERENCED", 4, "project", "close", "pr_data-room")
	if fmt.Sprint(details(t, eo)["referrers"]) != "["+id+"]" {
		t.Errorf("details %v", eo["details"])
	}
	outcomeOf(t, e.ok("task", "status", id, "done"), "updated")
	d := outcomeOf(t, e.ok("project", "close", "pr_data-room"), "updated")
	if d["live"] != false || readRecord(t, projectFile)["live"] != false {
		t.Errorf("closed %v", d)
	}
	outcomeOf(t, e.ok("project", "close", "pr_data-room"), "unchanged")
	if got := ids(t, e.ok("project", "list")); len(got) != 0 {
		t.Errorf("a closed project is not listed: %v", got)
	}
}

func TestWORK_INV_09_AClosedProjectAcceptsNoNewTasksAndReopens(t *testing.T) {
	e := projectWorld(t)
	e.ok(mkProject()...)
	id := idOf(t, e.ok(mkTask("One", "--project", "pr_data-room")...))
	e.ok("task", "status", id, "done")
	e.ok("project", "close", "pr_data-room")
	eo := e.refused("INVALID_VALUE", 2, mkTask("Two", "--project", "pr_data-room")...)
	if details(t, eo)["field"] != "project" || !strings.Contains(fmt.Sprint(eo["hint"]), "reopen") {
		t.Errorf("refusal %v", eo)
	}
	// A finished task cannot become live while its project is closed.
	e.refused("INVALID_VALUE", 2, "task", "status", id, "todo")
	e.ok("task", "status", id, "canceled")
	outcomeOf(t, e.ok("project", "reopen", "pr_data-room"), "updated")
	outcomeOf(t, e.ok("project", "reopen", "pr_data-room"), "unchanged")
	outcomeOf(t, e.ok(mkTask("Two", "--project", "pr_data-room")...), "created")
	outcomeOf(t, e.ok("task", "status", id, "todo"), "updated")
}

func TestCloseProjectCancelsLiveTasksAndIsSafeToRepeat(t *testing.T) {
	e := projectWorld(t)
	e.ok(mkProject()...)
	a := idOf(t, e.ok(mkTask("A", "--project", "pr_data-room")...))
	b := idOf(t, e.ok(mkTask("B", "--project", "pr_data-room")...))
	c := idOf(t, e.ok(mkTask("C", "--project", "pr_data-room")...))
	e.ok("task", "status", c, "done")
	d := outcomeOf(t, e.ok("project", "close", "pr_data-room", "--cancel-tasks"), "updated")
	got := fmt.Sprint(d["canceled_tasks"])
	if d["live"] != false || !strings.Contains(got, a) || !strings.Contains(got, b) || strings.Contains(got, c) {
		t.Errorf("closed %v", d)
	}
	for id, want := range map[string]string{a: "canceled", b: "canceled", c: "done"} {
		if s := data(t, e.ok("task", "get", id))["status"]; s != want {
			t.Errorf("%s is %v, want %s", id, s, want)
		}
	}
	d = outcomeOf(t, e.ok("project", "close", "pr_data-room", "--cancel-tasks"), "unchanged")
	if fmt.Sprint(d["canceled_tasks"]) != "[]" {
		t.Errorf("repeat %v", d)
	}
	// After a partial failure the project is still live and the rest finishes.
	rec := readRecord(t, projectFile)
	rec["live"] = true
	writeRecord(t, projectFile, rec)
	e.ok("project", "reopen", "pr_data-room")
	d = outcomeOf(t, e.ok("project", "close", "pr_data-room", "--cancel-tasks"), "updated")
	if d["live"] != false || fmt.Sprint(d["canceled_tasks"]) != "[]" {
		t.Errorf("resumed %v", d)
	}
	e.refused("NOT_FOUND", 3, as(otherAgent, "project", "close", "pr_data-room")...)
}

func TestWORK_INV_06_AFollowUpTakesTheParentOfItsInteraction(t *testing.T) {
	e := projectWorld(t)
	e.ok(mkProject("--deal", sierraDeal)...)
	e.ok("project", "create", "--name", "Second")
	// No deal or project: no parent.
	e.ok(record("Plain")...)
	plain := data(t, e.ok(mkTask("Follow plain", "--interaction", "it_2026-11-23-plain")...))
	if _, has := plain["project"]; has {
		t.Errorf("plain %v", plain)
	}
	// One deal: the follow-up takes it.
	e.ok(record("One deal", "--deal", sierraDeal)...)
	d := data(t, e.ok(mkTask("Follow deal", "--interaction", "it_2026-11-23-one-deal")...))
	if d["deal"] != sierraDeal || d["project"] != nil {
		t.Errorf("deal %v", d)
	}
	// One project: the follow-up takes it, and its deal.
	e.ok(record("One project", "--project", "pr_data-room")...)
	d = data(t, e.ok(mkTask("Follow project", "--interaction", "it_2026-11-23-one-project")...))
	if d["project"] != "pr_data-room" || d["deal"] != sierraDeal {
		t.Errorf("project %v", d)
	}
	// Several: the parent must be chosen, and the refusal lists them.
	e.ok(record("Both", "--deal", sierraDeal, "--project", "pr_data-room")...)
	eo := e.refused("INVALID_VALUE", 2, mkTask("Follow both", "--interaction", "it_2026-11-23-both")...)
	det := details(t, eo)
	if det["field"] != "project" || fmt.Sprint(det["allowed"]) != "["+sierraDeal+" pr_data-room]" {
		t.Errorf("details %v", det)
	}
	// An explicit parent wins.
	d = data(t, e.ok(mkTask("Follow both", "--interaction", "it_2026-11-23-both", "--project", "pr_second")...))
	if d["project"] != "pr_second" {
		t.Errorf("explicit %v", d)
	}
	// Repeating a create finds the task even when the interaction now has several parents.
	e.ok("interaction", "update", "it_2026-11-23-one-deal", "--project", "pr_data-room")
	outcomeOf(t, e.ok(mkTask("Follow deal", "--interaction", "it_2026-11-23-one-deal")...), "existing")
	e.refused("DEDUP_CONFLICT", 4, mkTask("Follow deal", "--interaction", "it_2026-11-23-one-deal", "--project", "pr_second")...)
}

func TestWORK_INV_08_RelatedRecordsAreOfTheAllowedTypesAndHolder(t *testing.T) {
	e := projectWorld(t)
	e.ok(mkProject("--related", elenaEmail, "--related", sierraID, "--related", sierraDeal)...)
	if got := fmt.Sprint(readRecord(t, projectFile)["related"]); got != "[dl_grupo-sierra org_grupo-sierra pe_elena-ruiz]" {
		t.Errorf("related %s", got)
	}
	id := idOf(t, e.ok(mkTask("T", "--related", "pr_data-room", "--related", elenaID, "--related", elenaID)...))
	if got := fmt.Sprint(readRecord(t, taskFile(t, id))["related"]); got != "[pe_elena-ruiz pr_data-room]" {
		t.Errorf("task related %s", got)
	}
	// Other holders' deals and projects, and other types, are refused.
	e.ok(as(otherAgent, "project", "create", "--name", "Their project")...)
	for _, args := range [][]string{
		mkTask("X", "--related", otherDeal), mkTask("X", "--related", "pr_their-project"),
		{"task", "update", id, "--related", otherDeal}, {"project", "update", "pr_data-room", "--related", "pr_their-project"},
	} {
		if eo := e.refused("HOLDER_MISMATCH", 2, args...); details(t, eo)["field"] != "related" {
			t.Errorf("%v: %v", args, eo["details"])
		}
	}
	e.refused("INVALID_VALUE", 2, mkTask("X", "--related", acmeHuman)...)
	e.refused("NOT_FOUND", 3, mkTask("X", "--related", "dl_ghost")...)
	// Update adds and removes, and related records take part in no other rule.
	outcomeOf(t, e.ok("task", "update", id, "--remove-related", elenaEmail, "--related", sierraDeal), "updated")
	if got := fmt.Sprint(readRecord(t, taskFile(t, id))["related"]); got != "[dl_grupo-sierra pr_data-room]" {
		t.Errorf("after update %s", got)
	}
	outcomeOf(t, e.ok("task", "update", id, "--related", sierraDeal), "unchanged")
	outcomeOf(t, e.ok("project", "update", "pr_data-room", "--remove-related", sierraDeal), "updated")
	e.ok("task", "status", id, "done")
	if d := data(t, e.ok("project", "get", "pr_data-room", "--full")); fmt.Sprint(d["related"]) != "[org_grupo-sierra pe_elena-ruiz]" {
		t.Errorf("full %v", d)
	}
}

func TestINTERACTIONS_INV_05_AnInteractionLinksDealsAndProjectsOfItsHolder(t *testing.T) {
	e := projectWorld(t)
	e.ok(mkProject()...)
	e.ok("project", "create", "--name", "Second")
	outcomeOf(t, e.ok(record("Linked", "--deal", sierraDeal, "--project", "pr_second", "--project", "pr_data-room")...), "created")
	if _, has := data(t, e.ok("interaction", "get", "it_2026-11-23-linked"))["deals"]; has {
		t.Error("the compact view shows the deals")
	}
	full := data(t, e.ok("interaction", "get", "it_2026-11-23-linked", "--full"))
	if fmt.Sprint(full["deals"]) != "["+sierraDeal+"]" || fmt.Sprint(full["projects"]) != "[pr_data-room pr_second]" {
		t.Errorf("full %v", full)
	}
	eo := e.refused("HOLDER_MISMATCH", 2, record("Bad", "--deal", otherDeal)...)
	if details(t, eo)["field"] != "deal" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("NOT_FOUND", 3, record("Bad", "--project", "pr_ghost")...)
	// Update links and unlinks.
	outcomeOf(t, e.ok("interaction", "update", "it_2026-11-23-linked", "--remove-project", "pr_second", "--remove-deal", sierraDeal), "updated")
	outcomeOf(t, e.ok("interaction", "update", "it_2026-11-23-linked", "--remove-project", "pr_second"), "unchanged")
	e.refused("HOLDER_MISMATCH", 2, "interaction", "update", "it_2026-11-23-linked", "--deal", otherDeal)
	// List filters, and a repeated create that lacks a link is a conflict.
	if got := ids(t, e.ok("interaction", "list", "--project", "pr_data-room")); len(got) != 1 {
		t.Errorf("by project %v", got)
	}
	if got := ids(t, e.ok("interaction", "list", "--deal", sierraDeal)); len(got) != 0 {
		t.Errorf("by deal %v", got)
	}
	e.ok(record("Mail", "--ref", msgRef)...)
	e.refused("DEDUP_CONFLICT", 4, record("Mail", "--ref", msgRef, "--deal", sierraDeal)...)
}

func TestWORKSPACE_AC_04_ProjectWritesReturnTheirOutcome(t *testing.T) {
	e := projectWorld(t)
	d := outcomeOf(t, e.ok(mkProject("--description-file", "-")...), "created")
	_ = d
	outcomeOf(t, e.ok("project", "update", "pr_data-room", "--name", "Data room 2"), "updated")
	outcomeOf(t, e.ok("project", "update", "pr_data-room", "--name", "Data room 2"), "unchanged")
	if v := readRecord(t, projectFile)["version"]; v != float64(2) {
		t.Errorf("version %v", v)
	}
	outcomeOf(t, e.runOK("About it\n", "project", "update", "pr_data-room", "--description-file", "-"), "updated")
	if g := data(t, e.ok("project", "get", "pr_data-room", "--full")); g["description"] != "About it\n" {
		t.Errorf("full %v", g)
	}
	if g := data(t, e.ok("project", "get", "pr_data-room")); strings.Join(keysOf(g), ",") != "holder,id,live,name" {
		t.Errorf("compact %v", keysOf(g))
	}
	e.ok("project", "close", "pr_data-room")
	outcomeOf(t, e.ok("project", "delete", "pr_data-room", "--full"), "deleted")
	e.refused("NOT_FOUND", 3, "project", "get", "pr_data-room")
	e.refused("UNKNOWN_FIELD", 2, "project", "update", "pr_data-room", "--live=false")
	e.refused("INVALID_VALUE", 2, "project", "create", "--name", "!!!")
	e.refused("INVALID_VALUE", 2, "project", "create")
}

func TestWORKSPACE_AC_05_ProjectsListByNameWithFiltersAndACursor(t *testing.T) {
	e := projectWorld(t)
	e.ok("project", "create", "--name", "Zeta", "--deal", sierraDeal)
	e.ok("project", "create", "--name", "Álamo")
	e.ok("project", "create", "--name", "Beta")
	e.ok("project", "close", "pr_beta")
	e.ok(as(otherAgent, "project", "create", "--name", "Gamma")...)
	all := ids(t, e.ok("project", "list", "--all"))
	if fmt.Sprint(all) != "[pr_alamo pr_beta pr_zeta]" {
		t.Errorf("all %v", all)
	}
	l := e.ok("project", "list")
	if fmt.Sprint(ids(t, l)) != "[pr_alamo pr_zeta]" || l["total"] != float64(2) {
		t.Errorf("live %v", l)
	}
	p1 := e.ok("project", "list", "--all", "--limit", "2")
	p2 := e.ok("project", "list", "--all", "--limit", "2", "--cursor", p1["next_cursor"].(string))
	if got := append(ids(t, p1), ids(t, p2)...); !slices.Equal(got, all) || p2["next_cursor"] != nil {
		t.Errorf("pages %v", got)
	}
	if got := ids(t, e.ok("project", "list", "--all-holders")); len(got) != 3 {
		t.Errorf("all holders %v", got)
	}
	e.refused("NOT_FOUND", 3, "project", "get", "pr_gamma")
}

func TestWORKSPACE_AC_10_DeletingADealOrProjectWithDependantsIsRefusedAndListsThem(t *testing.T) {
	e := projectWorld(t)
	e.ok(mkProject("--deal", sierraDeal)...)
	eo := e.refused("REFERENCED", 4, "deal", "delete", sierraDeal)
	if fmt.Sprint(details(t, eo)["referrers"]) != "[pr_data-room]" {
		t.Errorf("deal %v", eo["details"])
	}
	tk := idOf(t, e.ok(mkTask("T", "--project", "pr_data-room")...))
	eo = e.refused("REFERENCED", 4, "project", "delete", "pr_data-room")
	if fmt.Sprint(details(t, eo)["referrers"]) != "["+tk+"]" {
		t.Errorf("project %v", eo["details"])
	}
	// Interactions, related lists and task deals refer too.
	e.ok(record("Linked", "--project", "pr_data-room")...)
	e.ok("project", "create", "--name", "Other", "--related", "pr_data-room", "--related", elenaID)
	dt := idOf(t, e.ok(mkTask("D", "--deal", sierraDeal)...))
	got := fmt.Sprint(details(t, e.refused("REFERENCED", 4, "project", "delete", "pr_data-room"))["referrers"])
	for _, want := range []string{tk, "it_2026-11-23-linked", "pr_other"} {
		if !strings.Contains(got, want) {
			t.Errorf("referrers %s lack %s", got, want)
		}
	}
	if got := fmt.Sprint(details(t, e.refused("REFERENCED", 4, "deal", "delete", sierraDeal))["referrers"]); !strings.Contains(got, dt) {
		t.Errorf("deal referrers %s", got)
	}
	e.refused("REFERENCED", 4, "person", "delete", elenaID)
	// An own organization keeps its mark while it holds a project.
	e.ok(as(otherAgent, "project", "create", "--name", "Theirs")...)
	e.refused("REFERENCED", 4, "organization", "update", otherOrg, "--own=false")
	// Clearing the dependants lets the deletes through.
	e.ok("task", "delete", tk)
	e.ok("task", "delete", dt)
	e.ok("interaction", "delete", "it_2026-11-23-linked")
	e.ok("project", "delete", "pr_other")
	outcomeOf(t, e.ok("project", "delete", "pr_data-room"), "deleted")
	outcomeOf(t, e.ok("deal", "delete", sierraDeal), "deleted")
}

func TestCheckReportsProjectsAndParentLinks(t *testing.T) {
	e := projectWorld(t)
	if got := e.findings(); len(got) != 0 {
		t.Fatalf("findings %v", got)
	}
	e.ok(mkProject("--deal", sierraDeal, "--related", elenaID)...)
	tk := idOf(t, e.ok(mkTask("T", "--project", "pr_data-room", "--related", sierraDeal)...))
	e.ok(record("Linked", "--project", "pr_data-room", "--deal", sierraDeal)...)
	if got := e.findings(); len(got) != 0 {
		t.Fatalf("clean workspace has findings %v", got)
	}
	edit := func(path string, change func(map[string]any)) {
		rec := readRecord(t, path)
		change(rec)
		writeRecord(t, path, rec)
	}
	has := func(code, in string) {
		t.Helper()
		for _, m := range e.findings()[code] {
			if strings.Contains(m, in) {
				return
			}
		}
		t.Errorf("no %s finding with %q in %v", code, in, e.findings())
	}
	tf, it := taskFile(t, tk), "records/interactions/2026-11/it_2026-11-23-linked.json"
	// Dangling links.
	edit(projectFile, func(r map[string]any) { r["deal"] = "dl_ghost"; r["related"] = []string{"pe_ghost"} })
	edit(tf, func(r map[string]any) { r["related"] = []string{"dl_ghost2"} })
	edit(it, func(r map[string]any) { r["deals"] = []string{"dl_ghost3"}; r["projects"] = []string{"pr_ghost"} })
	has("DANGLING_REFERENCE", "dl_ghost ")
	has("DANGLING_REFERENCE", "pe_ghost")
	has("DANGLING_REFERENCE", "dl_ghost2")
	has("DANGLING_REFERENCE", "dl_ghost3")
	has("DANGLING_REFERENCE", "pr_ghost")
	// Links to another holder.
	edit(projectFile, func(r map[string]any) { r["deal"] = otherDeal; r["related"] = nil })
	edit(tf, func(r map[string]any) { r["related"] = []string{otherDeal} })
	edit(it, func(r map[string]any) { r["deals"] = []string{otherDeal}; delete(r, "projects") })
	has("HOLDER_MISMATCH", projectFile)
	if n := len(e.findings()["HOLDER_MISMATCH"]); n != 3 {
		t.Errorf("holder mismatches %v", e.findings()["HOLDER_MISMATCH"])
	}
	// Both parents, and a live task in a closed project.
	edit(projectFile, func(r map[string]any) { r["deal"] = sierraDeal; r["live"] = false })
	edit(tf, func(r map[string]any) { r["deal"] = sierraDeal; delete(r, "related") })
	edit(it, func(r map[string]any) { delete(r, "deals") })
	has("INVALID_RECORD", "at most one parent")
	has("INVALID_RECORD", "closed")
	// A project that breaks its rules.
	edit(projectFile, func(r map[string]any) { r["name"] = ""; r["related"] = []string{"us_ana-acme"}; delete(r, "live") })
	has("INVALID_RECORD", "needs a name")
	has("INVALID_RECORD", "related record")
	has("INVALID_RECORD", "live flag")
}
