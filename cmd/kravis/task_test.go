package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/diegomarino/kravis/internal/storage"
)

const (
	evaID  = "us_eva-acme"
	bot2ID = "us_bot2-acme" // Eva's agent
)

// taskWorld is a world where holder A also has a second human, Eva, with
// her own agent.
func taskWorld(t *testing.T) *env {
	t.Helper()
	e := world(t)
	e.ok("user", "create", "eva-acme", "--human", "--holder", orgID)
	e.ok("user", "create", "bot2-acme", "--agent", "--holder", orgID, "--handler", evaID)
	return e
}

// mkTask is the args of a task create for Ana; extra flags follow.
func mkTask(title string, extra ...string) []string {
	return slices.Concat([]string{"task", "create", "--title", title, "--assignee", acmeHuman}, extra)
}

func idOf(t *testing.T, out map[string]any) string {
	t.Helper()
	return data(t, out)["id"].(string)
}

// taskFile is where a task is stored.
func taskFile(t *testing.T, id string) string {
	t.Helper()
	m, err := filepath.Glob("records/tasks/*/" + id + ".json")
	if err != nil || len(m) != 1 {
		t.Fatalf("task file of %s: %v %v", id, m, err)
	}
	return filepath.ToSlash(m[0])
}

func writeRecord(t *testing.T, path string, rec map[string]any) {
	t.Helper()
	raw, err := json.MarshalIndent(rec, "", "  ")
	must(t, err)
	must(t, os.WriteFile(path, raw, 0o600))
}

func ids(t *testing.T, list map[string]any) []string {
	t.Helper()
	var out []string
	for _, r := range list["data"].([]any) {
		out = append(out, r.(map[string]any)["id"].(string))
	}
	return out
}

func originKey(interaction, purpose string) string {
	return storage.Key{Kind: "task-origin", Value: "task|" + interaction + "|" + purpose}.Path()
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestINTERACTIONS_AC_01_RecordingTheSameEmailAndItsFollowUpTwiceReturnsExisting(t *testing.T) {
	e := taskWorld(t)
	first := outcomeOf(t, e.ok(record("Data room documentation", "--ref", msgRef)...), "created")
	again := outcomeOf(t, e.ok(record("Data room documentation", "--ref", msgRef)...), "existing")
	if first["id"] != dataRoomID || again["id"] != dataRoomID {
		t.Fatalf("ids %v %v", first["id"], again["id"])
	}

	args := mkTask("Send the teaser to Elena", "--interaction", dataRoomID, "--agent", acmeAgent, "--due", "2026-11-28", "--fields", "from,purpose")
	made := e.ok(args...)
	d := outcomeOf(t, made, "created")
	if d["from"] != dataRoomID || d["purpose"] != "follow-up" || d["status"] != "todo" || d["holder"] != orgID || d["assignee"] != acmeHuman {
		t.Errorf("created: %v", d)
	}
	id := d["id"].(string)
	before := e.Files()
	d = outcomeOf(t, e.ok(args...), "existing")
	if d["id"] != id {
		t.Errorf("existing task %v, want %s", d["id"], id)
	}
	if got := e.Files(); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Error("an existing result changed a file")
	}
	if l := e.ok("task", "list"); l["total"] != float64(1) {
		t.Errorf("tasks %v", l["total"])
	}
	if !exists(originKey(dataRoomID, "follow-up")) {
		t.Error("no task-origin key")
	}
}

func TestWORK_INV_05_ATaskFromAnInteractionHasAPurposeAndOneTaskPerInteractionAndPurpose(t *testing.T) {
	e := taskWorld(t)
	e.ok(record("Data room documentation")...)
	d := outcomeOf(t, e.ok(mkTask("Follow up", "--interaction", dataRoomID, "--fields", "from,purpose")...), "created")
	id := d["id"].(string)
	if d["purpose"] != "follow-up" {
		t.Errorf("default purpose %v", d["purpose"])
	}
	// Another purpose of the same interaction is another task.
	d2 := outcomeOf(t, e.ok(mkTask("Ping", "--interaction", dataRoomID, "--purpose", "ping", "--fields", "purpose")...), "created")
	if d2["id"] == id || d2["purpose"] != "ping" {
		t.Errorf("ping task %v", d2)
	}
	// The same origin with contradicting data is a conflict, and creates nothing.
	for _, args := range [][]string{
		mkTask("Another title", "--interaction", dataRoomID),
		slices.Concat([]string{"task", "create", "--title", "Follow up", "--assignee", evaID, "--interaction", dataRoomID}),
		mkTask("Follow up", "--interaction", dataRoomID, "--agent", acmeAgent),
		mkTask("Follow up", "--interaction", dataRoomID, "--due", "2026-12-01"),
	} {
		eo := e.refused("DEDUP_CONFLICT", 4, args...)
		if details(t, eo)["id"] != id {
			t.Errorf("details %v", eo["details"])
		}
	}
	// A purpose without an interaction is kept, and deduplicates nothing.
	a := outcomeOf(t, e.ok(mkTask("Prepare the call", "--purpose", "prepare")...), "created")
	b := outcomeOf(t, e.ok(mkTask("Prepare the call", "--purpose", "prepare")...), "created")
	if a["id"] == b["id"] {
		t.Error("a task without an origin has no natural key")
	}
	// The origin and the purpose never change.
	e.refused("UNKNOWN_FIELD", 2, "task", "update", id, "--purpose", "ping")
	e.refused("UNKNOWN_FIELD", 2, "task", "update", id, "--interaction", dataRoomID)
	// An interaction that does not exist, or belongs to another holder.
	e.refused("NOT_FOUND", 3, mkTask("x", "--interaction", "it_2026-01-01-nothing")...)
	other := idOf(t, e.ok(as(otherAgent, record("Other holder's note")...)...))
	eo := e.refused("HOLDER_MISMATCH", 2, mkTask("x", "--interaction", other)...)
	if details(t, eo)["field"] != "interaction" {
		t.Errorf("details %v", eo["details"])
	}
	// The key follows the task: deleting it frees the origin.
	outcomeOf(t, e.ok("task", "delete", id), "deleted")
	if exists(originKey(dataRoomID, "follow-up")) {
		t.Error("the key outlived the task")
	}
	if got := outcomeOf(t, e.ok(mkTask("Follow up", "--interaction", dataRoomID)...), "created"); got["id"] == id {
		t.Error("a deleted task came back")
	}
}

func TestWORK_INV_03_ATasksAssigneeIsALiveHumanOfItsHolder(t *testing.T) {
	e := taskWorld(t)
	e.ok("user", "create", "old-acme", "--human", "--holder", orgID)
	e.ok("user", "update", "us_old-acme", "--live=false")
	// No assignee at all, and an empty one.
	e.refused("INVALID_VALUE", 2, "task", "create", "--title", "x")
	e.refused("INVALID_VALUE", 2, "task", "create", "--title", "x", "--assignee", "")
	// Not live.
	eo := e.refused("INVALID_VALUE", 2, "task", "create", "--title", "x", "--assignee", "us_old-acme")
	if details(t, eo)["field"] != "assignee" {
		t.Errorf("details %v", eo["details"])
	}
	// An assignee of the other holder.
	eo = e.refused("HOLDER_MISMATCH", 2, "task", "create", "--title", "x", "--assignee", otherHuman)
	if details(t, eo)["field"] != "assignee" {
		t.Errorf("details %v", eo["details"])
	}
	// An agent is not a human, and a stranger does not exist.
	e.refused("INVALID_VALUE", 2, "task", "create", "--title", "x", "--assignee", acmeAgent)
	e.refused("NOT_FOUND", 3, "task", "create", "--title", "x", "--assignee", "us_nobody")
	// On update too.
	id := idOf(t, e.ok(mkTask("Real")...))
	e.refused("INVALID_VALUE", 2, "task", "update", id, "--assignee", "us_old-acme")
	e.refused("HOLDER_MISMATCH", 2, "task", "update", id, "--assignee", otherHuman)
	e.refused("INVALID_VALUE", 2, "task", "update", id, "--assignee", acmeAgent)
	outcomeOf(t, e.ok("task", "update", id, "--assignee", evaID), "updated")
	// A task keeps working after its assignee is deactivated, once it is done.
	e.ok("task", "status", id, "done")
	e.ok("user", "update", bot2ID, "--live=false")
	e.ok("user", "update", evaID, "--live=false")
	outcomeOf(t, e.ok("task", "update", id, "--title", "Renamed"), "updated")
}

func TestWORK_INV_04_AnAssignedAgentIsHandledByTheAssignee(t *testing.T) {
	e := taskWorld(t)
	// An agent of another human, of another holder, and a human.
	eo := e.refused("INVALID_VALUE", 2, mkTask("x", "--agent", bot2ID)...)
	if details(t, eo)["field"] != "agent" {
		t.Errorf("details %v", eo["details"])
	}
	eo = e.refused("HOLDER_MISMATCH", 2, mkTask("x", "--agent", otherAgent)...)
	if details(t, eo)["field"] != "agent" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("INVALID_VALUE", 2, mkTask("x", "--agent", evaID)...)
	e.refused("NOT_FOUND", 3, mkTask("x", "--agent", "us_nobody")...)
	e.ok("user", "create", "gone-acme", "--agent", "--holder", orgID, "--handler", userID)
	e.ok("user", "update", "us_gone-acme", "--live=false")
	e.refused("INVALID_VALUE", 2, mkTask("x", "--agent", "us_gone-acme")...)

	id := idOf(t, e.ok(mkTask("With an agent", "--agent", acmeAgent)...))
	// Changing the assignee would leave the agent with a stranger.
	eo = e.refused("INVALID_VALUE", 2, "task", "update", id, "--assignee", evaID)
	if details(t, eo)["field"] != "assignee" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("INVALID_VALUE", 2, "task", "update", id, "--agent", bot2ID)
	// Clearing the agent, or giving the new assignee's own, is fine.
	d := outcomeOf(t, e.ok("task", "update", id, "--assignee", evaID, "--agent", ""), "updated")
	if d["assignee"] != evaID || d["agent"] != nil {
		t.Errorf("updated %v", d)
	}
	d = outcomeOf(t, e.ok("task", "update", id, "--agent", bot2ID), "updated")
	if d["agent"] != bot2ID {
		t.Errorf("updated %v", d)
	}
	// An assignee without an agent can take one later.
	id2 := idOf(t, e.ok(mkTask("Alone")...))
	outcomeOf(t, e.ok("task", "update", id2, "--agent", acmeAgent), "updated")
}

func TestWORK_INV_07_ATaskChangesStatusOnlyThroughItsTransition(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok(mkTask("Move me")...))
	before := e.Files()
	eo := e.refused("INVALID_VALUE", 2, "task", "update", id, "--status", "done")
	if details(t, eo)["field"] != "status" || !strings.Contains(eo["hint"].(string), "task status") {
		t.Errorf("error %v", eo)
	}
	if fmt.Sprint(e.Files()) != fmt.Sprint(before) {
		t.Error("refused update changed a file")
	}
	d := outcomeOf(t, e.ok("task", "status", id, "done"), "updated")
	if d["status"] != "done" || d["category"] != "done" || d["live"] != false {
		t.Errorf("done: %v", d)
	}
	version := readRecord(t, taskFile(t, id))["version"]
	outcomeOf(t, e.ok("task", "status", id, "done"), "unchanged")
	if got := readRecord(t, taskFile(t, id))["version"]; got != version {
		t.Errorf("version %v became %v", version, got)
	}
	// The status is not a field of update, but a refusal even for its current value.
	e.refused("INVALID_VALUE", 2, "task", "update", id, "--status", "done")
}

func TestWORKSPACE_AC_13_ATaskStatusOrPurposeOutsideTheCatalogIsRefusedWithTheAllowedList(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok(mkTask("x")...))
	eo := e.refused("INVALID_VALUE", 2, "task", "status", id, "pending")
	det := details(t, eo)
	if det["field"] != "status" || fmt.Sprint(det["allowed"]) != "[todo in-progress waiting done canceled]" {
		t.Errorf("details %v", det)
	}
	eo = e.refused("INVALID_VALUE", 2, mkTask("x", "--purpose", "nag")...)
	det = details(t, eo)
	if det["field"] != "purpose" || fmt.Sprint(det["allowed"]) != "[follow-up ping expand intro prepare]" {
		t.Errorf("details %v", det)
	}
	e.refused("INVALID_VALUE", 2, "task", "list", "--category", "paused")
}

func TestWORKSPACE_INV_06_ATaskStoresItsCategoryAndLiveFromTheCatalog(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok(mkTask("x")...))
	for _, c := range []struct {
		status, category string
		live             bool
	}{
		{"todo", "open", true}, {"in-progress", "open", true}, {"waiting", "waiting", true},
		{"done", "done", false}, {"canceled", "canceled", false},
	} {
		status, want := c.status, [2]any{c.category, c.live}
		e.ok("task", "status", id, status)
		rec := readRecord(t, taskFile(t, id))
		if rec["category"] != want[0] || rec["live"] != want[1] {
			t.Errorf("%s: %v %v, want %v", status, rec["category"], rec["live"], want)
		}
	}
	// A record that disagrees with the catalog is reported, and so is one that disagrees with itself.
	rec := readRecord(t, taskFile(t, id)) // canceled
	rec["category"] = "open"
	writeRecord(t, taskFile(t, id), rec)
	got := e.findings()
	if len(got["INVALID_RECORD"]) == 0 || !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "disagrees with the catalog") {
		t.Errorf("category: %v", got)
	}
	rec["category"], rec["live"] = "canceled", true
	writeRecord(t, taskFile(t, id), rec)
	got = e.findings()
	if len(got["INVALID_RECORD"]) == 0 {
		t.Errorf("live: %v", got)
	}
	// A catalog that moves a status to another category is seen as a disagreement.
	rec["live"] = false
	writeRecord(t, taskFile(t, id), rec)
	if got := e.findings(); len(got) != 0 {
		t.Fatalf("clean again: %v", got)
	}
	e.setStatuses(`[{"name":"todo","category":"open"},{"name":"canceled","category":"done"}]`)
	if got := e.findings(); len(got["INVALID_RECORD"]) == 0 {
		t.Errorf("catalog moved: %v", got)
	}
}

// setStatuses replaces the task status catalog with the given JSON.
func (e *env) setStatuses(statuses string) {
	e.t.Helper()
	var cfg map[string]any
	raw, err := os.ReadFile("config.json")
	must(e.t, err)
	must(e.t, json.Unmarshal(raw, &cfg))
	var list []any
	must(e.t, json.Unmarshal([]byte(statuses), &list))
	cfg["catalogs"].(map[string]any)["task_statuses"] = list
	writeRecord(e.t, "config.json", cfg)
}

func (e *env) setPurposes(purposes ...string) {
	e.t.Helper()
	var cfg map[string]any
	raw, err := os.ReadFile("config.json")
	must(e.t, err)
	must(e.t, json.Unmarshal(raw, &cfg))
	cfg["catalogs"].(map[string]any)["purposes"] = purposes
	writeRecord(e.t, "config.json", cfg)
}

func TestWORKSPACE_INV_05_AStatusOrPurposeRemovedFromTheCatalogStaysAndMustBeReplaced(t *testing.T) {
	e := taskWorld(t)
	e.ok(record("Data room documentation")...)
	id := idOf(t, e.ok(mkTask("Chase", "--interaction", dataRoomID)...))
	e.ok("task", "status", id, "waiting")
	// "waiting" leaves the catalog while the task is in it.
	e.setStatuses(`[{"name":"todo","category":"open"},{"name":"done","category":"done"}]`)
	got := e.findings()
	if r := strings.Join(got["VALUE_NOT_IN_CATALOG"], "\n"); !strings.Contains(r, `"waiting"`) || !strings.Contains(r, orgID) {
		t.Errorf("findings %v", got)
	}
	// The task can still be listed, read and deleted, but an edit must replace the status first.
	e.ok("task", "list")
	e.ok("task", "get", id)
	eo := e.refused("INVALID_VALUE", 2, "task", "update", id, "--title", "Chase harder")
	det := details(t, eo)
	if det["field"] != "status" || fmt.Sprint(det["allowed"]) != "[todo done]" {
		t.Errorf("details %v", det)
	}
	e.refused("INVALID_VALUE", 2, "task", "status", id, "waiting")
	d := outcomeOf(t, e.ok("task", "status", id, "todo"), "updated")
	if d["status"] != "todo" {
		t.Errorf("status %v", d["status"])
	}
	outcomeOf(t, e.ok("task", "update", id, "--title", "Chase harder"), "updated")
	if got := e.findings(); len(got) != 0 {
		t.Errorf("clean again: %v", got)
	}
	// A purpose that left the catalog is reported too. It is part of the
	// task's origin, so it cannot be replaced: the task stays editable.
	e.setPurposes("ping", "expand")
	if r := strings.Join(e.findings()["VALUE_NOT_IN_CATALOG"], "\n"); !strings.Contains(r, `"follow-up"`) {
		t.Errorf("purpose findings %v", r)
	}
	outcomeOf(t, e.ok("task", "update", id, "--title", "Chase even harder"), "updated")
	e.refused("INVALID_VALUE", 2, mkTask("New", "--purpose", "follow-up")...)
}

func TestWORKSPACE_AC_04_ATaskWriteReturnsItsOutcomeAndCompactView(t *testing.T) {
	e := taskWorld(t)
	out := e.ok(mkTask("Outcomes", "--agent", acmeAgent, "--due", "2026-12-24")...)
	d := outcomeOf(t, out, "created")
	if fmt.Sprint(keysOf(d)) != "[agent assignee category due holder id live status title]" {
		t.Errorf("compact view %v", keysOf(d))
	}
	id := d["id"].(string)
	e.refused("INVALID_VALUE", 2, "task", "update", id, "--due", "tomorrow")
	outcomeOf(t, e.ok("task", "update", id, "--title", "Outcomes"), "unchanged")
	outcomeOf(t, e.ok("task", "update", id, "--due", ""), "updated")
	if _, has := data(t, e.ok("task", "get", id))["due"]; has {
		t.Error("an empty --due clears the date")
	}
	d = outcomeOf(t, e.ok("task", "delete", id), "deleted")
	if d["id"] != id {
		t.Errorf("deleted %v", d)
	}
	e.refused("NOT_FOUND", 3, "task", "get", id)
	e.refused("NOT_FOUND", 3, "task", "get", "not-a-task")
	e.refused("UNKNOWN_FIELD", 2, "task", "update", id, "--owner", userID)
	// The synonyms of other products point to Kravis's words.
	eo := e.refused("UNKNOWN_FIELD", 2, "task", "create", "--title", "x", "--owner", userID)
	if !strings.Contains(eo["hint"].(string), "--assignee") {
		t.Errorf("hint %v", eo["hint"])
	}
	eo = e.refused("UNKNOWN_FIELD", 2, "task", "create", "--title", "x", "--delegate", acmeAgent)
	if !strings.Contains(eo["hint"].(string), "--agent") {
		t.Errorf("hint %v", eo["hint"])
	}
	e.refused("INVALID_VALUE", 2, "task", "create", "--assignee", userID)
	e.refused("INVALID_VALUE", 2, "task", "create", "--title", "  ", "--assignee", userID)
	e.refused("INVALID_VALUE", 2, mkTask("x", "--due", "2026-13-40")...)
	e.refused("UNKNOWN_FIELD", 2, "task", "create", "--title", "x", "--assignee", userID, "--fields", "nope")
}

func TestTaskDescriptionLivesInTheSidecar(t *testing.T) {
	e := taskWorld(t)
	file := filepath.Join(e.Home, "desc.md")
	must(t, os.WriteFile(file, []byte("Send the teaser.\n"), 0o600))
	d := outcomeOf(t, e.ok(mkTask("Teaser", "--description-file", file)...), "created")
	id := d["id"].(string)
	if d["description"] != "Send the teaser.\n" {
		t.Errorf("created view %v", d)
	}
	sidecar := strings.TrimSuffix(taskFile(t, id), ".json") + ".md"
	if raw, err := os.ReadFile(sidecar); err != nil || string(raw) != "Send the teaser.\n" {
		t.Errorf("sidecar %q %v", raw, err)
	}
	if _, has := data(t, e.ok("task", "get", id))["description"]; has {
		t.Error("the compact view carries no description")
	}
	if got := data(t, e.ok("task", "get", id, "--fields", "description"))["description"]; got != "Send the teaser.\n" {
		t.Errorf("get %v", got)
	}
	l := e.ok("task", "list", "--full")
	if got := l["data"].([]any)[0].(map[string]any)["description"]; got != "Send the teaser.\n" {
		t.Errorf("list %v", got)
	}
	outcomeOf(t, e.ok("task", "update", id, "--description-file", file), "unchanged")
	r := e.runWithStdin("From stdin", "task", "update", id, "--description-file", "-")
	if r.Code != 0 {
		t.Fatal(r.Stderr)
	}
	if got := data(t, e.ok("task", "get", id, "--full"))["description"]; got != "From stdin" {
		t.Errorf("updated %v", got)
	}
	outcomeOf(t, e.ok("task", "update", id, "--description-file", ""), "updated")
	if exists(sidecar) {
		t.Error("an empty description removes the sidecar")
	}
	e.refused("INVALID_VALUE", 2, "task", "update", id, "--description-file", filepath.Join(e.Home, "missing.md"))
	// A deleted task shows what it was, description included, and takes its sidecar.
	e.ok("task", "update", id, "--description-file", file)
	if d := outcomeOf(t, e.ok("task", "delete", id, "--full"), "deleted"); d["description"] != "Send the teaser.\n" {
		t.Errorf("deleted %v", d)
	}
	if exists(sidecar) {
		t.Error("the sidecar outlived the task")
	}
}

func TestTaskFileIsStoredInTheUTCMonthOfItsTypeID(t *testing.T) {
	e := taskWorld(t)
	// The workspace clock says November 2026; the folder comes from the
	// TypeID, which holds the real time of the create, in UTC.
	id := idOf(t, e.ok(mkTask("Where am I", "--agent", acmeAgent, "--due", "2026-11-28")...))
	want := "records/tasks/" + time.Now().UTC().Format("2006-01") + "/" + id + ".json"
	if got := taskFile(t, id); got != want {
		t.Errorf("stored at %s, want %s", got, want)
	}
	rec := readRecord(t, want)
	if fmt.Sprintf("%v %v %v %v", rec["status"], rec["category"], rec["live"], rec["version"]) != "todo open true 1" || rec["holder"] != orgID {
		t.Errorf("record %v", rec)
	}
}

// fixture is one task of the list dataset.
type fixture struct {
	id, holder, assignee, agent, due, status string
}

// dataset creates 60 tasks, with counterexamples for every filter, and
// returns them. Today is 2026-11-24 in Madrid.
func dataset(t *testing.T, e *env) []fixture {
	t.Helper()
	var all []fixture
	add := func(holder, actor, assignee, agent, due, status string) {
		args := []string{"task", "create", "--title", fmt.Sprintf("Task %d", len(all)), "--assignee", assignee}
		if agent != "" {
			args = append(args, "--agent", agent)
		}
		if due != "" {
			args = append(args, "--due", due)
		}
		id := idOf(t, e.ok(as(actor, args...)...))
		if status != "todo" {
			e.ok(as(actor, "task", "status", id, status)...)
		}
		all = append(all, fixture{id, holder, assignee, agent, due, status})
	}
	for i := 10; i < 20; i++ { // Ana's agent, overdue
		add(orgID, acmeHuman, acmeHuman, acmeAgent, fmt.Sprintf("2026-11-%02d", i), "todo")
	}
	for i := 1; i <= 10; i++ { // Eva, with her agent, overdue
		add(orgID, acmeHuman, evaID, bot2ID, fmt.Sprintf("2026-11-%02d", i), "in-progress")
	}
	for range 5 { // due tomorrow
		add(orgID, acmeHuman, acmeHuman, "", "2026-11-25", "todo")
	}
	for range 5 { // no due date
		add(orgID, acmeHuman, acmeHuman, "", "", "waiting")
	}
	for range 5 { // done, long past due
		add(orgID, acmeHuman, acmeHuman, "", "2026-11-05", "done")
	}
	for range 3 { // canceled, long past due
		add(orgID, acmeHuman, evaID, "", "2026-11-06", "canceled")
	}
	for range 5 { // due today
		add(orgID, acmeHuman, acmeHuman, "", "2026-11-24", "todo")
	}
	for range 7 { // due next month, Ana's agent
		add(orgID, acmeHuman, acmeHuman, acmeAgent, "2026-12-15", "todo")
	}
	for range 10 { // the other holder's, overdue
		add(otherOrg, otherAgent, otherHuman, otherAgent, "2026-11-02", "todo")
	}
	if len(all) != 60 {
		t.Fatalf("%d fixtures", len(all))
	}
	return all
}

// want is the IDs of the fixtures that match, sorted.
func want(all []fixture, keep func(fixture) bool) []string {
	var out []string
	for _, f := range all {
		if keep(f) {
			out = append(out, f.id)
		}
	}
	slices.Sort(out)
	return out
}

func sortedIDs(t *testing.T, list map[string]any) []string {
	t.Helper()
	out := ids(t, list)
	slices.Sort(out)
	return out
}

func live(f fixture) bool { return f.status != "done" && f.status != "canceled" }

func TestWORK_AC_02_ATaskListShowsLiveTasksAndFilters(t *testing.T) {
	e := taskWorld(t)
	all := dataset(t, e)
	mine := func(f fixture) bool { return f.holder == orgID }
	check := func(label string, got map[string]any, keep func(fixture) bool) {
		t.Helper()
		w := want(all, keep)
		if g := sortedIDs(t, got); !slices.Equal(g, w) {
			t.Errorf("%s: %d tasks %v, want %d %v", label, len(g), g, len(w), w)
		}
		if got["total"] != float64(len(w)) {
			t.Errorf("%s: total %v, want %d", label, got["total"], len(w))
		}
	}
	list := func(args ...string) map[string]any {
		return e.ok(slices.Concat([]string{"task", "list", "--limit", "0"}, args)...)
	}

	// Live tasks of the actor's holder by default.
	check("default", list(), func(f fixture) bool { return mine(f) && live(f) })
	if got := list(); got["total"] != float64(42) || obj(t, got, "filters")["holder"] != orgID {
		t.Errorf("default: %v %v", got["total"], got["filters"])
	}
	check("all", list("--all"), mine)
	check("assignee", list("--assignee", evaID), func(f fixture) bool { return mine(f) && live(f) && f.assignee == evaID })
	check("assignee, all", list("--assignee", evaID, "--all"), func(f fixture) bool { return mine(f) && f.assignee == evaID })
	check("agent", list("--agent", acmeAgent), func(f fixture) bool { return mine(f) && live(f) && f.agent == acmeAgent })
	if got := list("--agent", acmeAgent); got["total"] != float64(17) {
		t.Errorf("agent: %v", got["total"])
	}
	overdue := func(f fixture) bool { return mine(f) && live(f) && f.due != "" && f.due < "2026-11-24" }
	check("overdue", list("--overdue"), overdue)
	if got := list("--overdue"); got["total"] != float64(20) {
		t.Errorf("overdue: %v", got["total"])
	}
	check("overdue, all", list("--overdue", "--all"), overdue) // done and canceled are never overdue
	check("overdue and assignee", list("--overdue", "--assignee", acmeHuman), func(f fixture) bool { return overdue(f) && f.assignee == acmeHuman })
	before := func(f fixture) bool { return mine(f) && live(f) && f.due != "" && f.due < "2026-11-25" }
	check("due-before", list("--due-before", "2026-11-25"), before)
	if got := list("--due-before", "2026-11-25"); got["total"] != float64(25) {
		t.Errorf("due-before: %v", got["total"])
	}
	check("due-before, all", list("--due-before", "2026-11-25", "--all"), func(f fixture) bool { return mine(f) && f.due != "" && f.due < "2026-11-25" })
	check("category done", list("--category", "done"), func(f fixture) bool { return mine(f) && f.status == "done" })
	check("category canceled", list("--category", "canceled"), func(f fixture) bool { return mine(f) && f.status == "canceled" })
	check("category waiting", list("--category", "waiting"), func(f fixture) bool { return mine(f) && f.status == "waiting" })
	check("category open", list("--category", "open"), func(f fixture) bool { return mine(f) && (f.status == "todo" || f.status == "in-progress") })
	// The other holder is seen only on request, or by its own actors.
	check("all holders", list("--all-holders", "--all"), func(fixture) bool { return true })
	check("other actor", e.ok(as(otherAgent, "task", "list", "--limit", "0")...), func(f fixture) bool { return !mine(f) })
	check("other actor, overdue", e.ok(as(otherAgent, "task", "list", "--overdue", "--limit", "0")...), func(f fixture) bool { return !mine(f) })
	e.noActor()
	check("no actor", list("--all"), func(fixture) bool { return true })
	e.ok("whoami", "--set", userID)

	// Soonest due first, tasks without a due date last, the ID breaking ties.
	var dues []string
	for _, r := range list()["data"].([]any) {
		d, _ := r.(map[string]any)["due"].(string)
		dues = append(dues, d)
	}
	if !slices.IsSortedFunc(dues[:len(dues)-5], strings.Compare) || slices.Contains(dues[:len(dues)-5], "") || slices.Max(dues[len(dues)-5:]) != "" {
		t.Errorf("order %v", dues)
	}

	// A page followed with --cursor lists every task once, in the same order.
	full := ids(t, list())
	var paged []string
	args := []string{"task", "list", "--limit", "10"}
	for pages := 0; ; pages++ {
		page := e.ok(args...)
		if page["total"] != float64(42) {
			t.Fatalf("total %v", page["total"])
		}
		paged = append(paged, ids(t, page)...)
		next, _ := page["next_cursor"].(string)
		if next == "" {
			if pages != 4 {
				t.Errorf("%d pages", pages+1)
			}
			break
		}
		args = []string{"task", "list", "--limit", "10", "--cursor", next}
	}
	if !slices.Equal(paged, full) {
		t.Errorf("paged %v\nfull  %v", paged, full)
	}
	// A cursor belongs to its filters.
	page := e.ok("task", "list", "--limit", "5")
	e.refused("INVALID_VALUE", 2, "task", "list", "--limit", "5", "--overdue", "--cursor", page["next_cursor"].(string))
	e.refused("INVALID_VALUE", 2, "task", "list", "--due-before", "soon")
}

func TestWORKSPACE_AC_05_ATaskListIsOrderedAndPaged(t *testing.T) {
	e := taskWorld(t)
	for i, due := range []string{"2026-12-02", "", "2026-12-01", "2026-12-02", ""} {
		args := mkTask(fmt.Sprintf("T%d", i))
		if due != "" {
			args = append(args, "--due", due)
		}
		e.ok(args...)
	}
	out := e.ok("task", "list", "--limit", "2")
	if len(ids(t, out)) != 2 || out["total"] != float64(5) || out["next_cursor"] == nil {
		t.Errorf("first page %v", out)
	}
	all := e.ok("task", "list", "--limit", "0")
	if all["next_cursor"] != nil || len(ids(t, all)) != 5 {
		t.Errorf("all %v", all)
	}
	rows := all["data"].([]any)
	var dues []any
	for _, r := range rows {
		dues = append(dues, r.(map[string]any)["due"])
	}
	if fmt.Sprint(dues) != "[2026-12-01 2026-12-02 2026-12-02 <nil> <nil>]" {
		t.Errorf("order %v", dues)
	}
	e.refused("INVALID_VALUE", 2, "task", "list", "--limit", "0", "--cursor", "x")
	e.refused("INVALID_VALUE", 2, "task", "list", "--limit", "-1")
}

func TestWORKSPACE_INV_13_TasksNotLiveAreListedOnRequest(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok(mkTask("Finish me")...))
	e.ok("task", "status", id, "done")
	if l := e.ok("task", "list"); l["total"] != float64(0) {
		t.Errorf("live list %v", l)
	}
	if l := e.ok("task", "list", "--all"); l["total"] != float64(1) {
		t.Errorf("--all %v", l)
	}
	if l := e.ok("task", "list", "--category", "done"); l["total"] != float64(1) {
		t.Errorf("--category %v", l)
	}
	e.ok("task", "get", id) // a task not live can still be read
}

func TestOverdueUsesTheWorkspaceTimezone(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok(mkTask("Due on the 24th", "--due", "2026-11-24")...))
	for _, c := range []struct {
		name    string
		now     time.Time
		overdue bool
	}{
		{"23:30 in Madrid on the 24th", time.Date(2026, 11, 24, 22, 30, 0, 0, time.UTC), false},
		{"00:30 in Madrid on the 25th", time.Date(2026, 11, 24, 23, 30, 0, 0, time.UTC), true},
		{"08:00 in Madrid on the 24th", time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC), false},
	} {
		e.Now = c.now
		got := ids(t, e.ok("task", "list", "--overdue"))
		if (len(got) == 1 && got[0] == id) != c.overdue {
			t.Errorf("%s: overdue %v, want %v", c.name, got, c.overdue)
		}
	}
}

func TestWORKSPACE_INV_02_AnActorWritesOnlyItsOwnHoldersTasks(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok(mkTask("Holder A's task")...))
	for _, flags := range [][]string{nil, {"--all-holders"}} {
		for _, cmd := range [][]string{
			{"task", "update", id, "--title", "Taken over"},
			{"task", "status", id, "done"},
			{"task", "delete", id},
		} {
			e.refused("NOT_FOUND", 3, as(otherAgent, slices.Concat(cmd, flags)...)...)
		}
	}
	if rec := readRecord(t, taskFile(t, id)); rec["title"] != "Holder A's task" || rec["status"] != "todo" || rec["version"] != float64(1) {
		t.Errorf("changed: %v", rec)
	}
	// The other holder cannot read it either, unless it asks for all holders.
	e.refused("NOT_FOUND", 3, as(otherAgent, "task", "get", id)...)
	e.ok(as(otherAgent, "task", "get", id, "--all-holders")...)
	// A task takes its holder from its actor.
	d := outcomeOf(t, e.ok(as(otherAgent, "task", "create", "--title", "B's", "--assignee", otherHuman)...), "created")
	if d["holder"] != otherOrg {
		t.Errorf("holder %v", d["holder"])
	}
	// And the holders of the people it names must match.
	e.refused("HOLDER_MISMATCH", 2, as(otherAgent, mkTask("x")...)...)
}

func TestWORKSPACE_INV_11_AUserReferencedByTasksIsNotDeletedOrDeactivated(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok("task", "create", "--title", "For Eva", "--assignee", evaID, "--agent", bot2ID))
	e.ok("user", "update", bot2ID, "--name", "Eva's agent")
	// Deactivating the assignee or the agent of a live task is refused; the task is listed.
	e.refused("REFERENCED", 4, "user", "update", bot2ID, "--live=false")
	eo := e.refused("REFERENCED", 4, "user", "update", evaID, "--live=false")
	if fmt.Sprint(details(t, eo)["referrers"]) != "[us_bot2-acme]" { // the live agent is named first
		t.Errorf("details %v", eo["details"])
	}
	e.ok("user", "update", bot2ID, "--name", "Eva's agent")
	// A human with a live task and no agent.
	id2 := idOf(t, e.ok("task", "create", "--title", "For Tom", "--assignee", tomID(t, e)))
	eo = e.refused("REFERENCED", 4, "user", "update", tomID(t, e), "--live=false")
	if fmt.Sprint(details(t, eo)["referrers"]) != "["+id2+"]" {
		t.Errorf("details %v", eo["details"])
	}
	// A task that is done is kept, and still stops the delete, but not the deactivation.
	e.ok("task", "status", id2, "done")
	eo = e.refused("REFERENCED", 4, "user", "delete", tomID(t, e))
	if fmt.Sprint(details(t, eo)["referrers"]) != "["+id2+"]" {
		t.Errorf("details %v", eo["details"])
	}
	e.ok("user", "update", tomID(t, e), "--live=false")
	// The holder is referenced by its tasks as well.
	e.ok("task", "delete", id2)
	e.ok("task", "delete", id)
	e.ok("user", "delete", tomID(t, e))
}

func tomID(t *testing.T, e *env) string {
	t.Helper()
	if !exists("records/users/us_tom-acme.json") {
		e.ok("user", "create", "tom-acme", "--human", "--holder", orgID)
	}
	return "us_tom-acme"
}

func TestWORKSPACE_AC_10_DeletingAnInteractionATaskComesFromIsRefusedAndListsTheTask(t *testing.T) {
	e := taskWorld(t)
	e.ok(record("Data room documentation", "--ref", msgRef)...)
	id := idOf(t, e.ok(mkTask("Follow up", "--interaction", dataRoomID)...))
	eo := e.refused("REFERENCED", 4, "interaction", "delete", dataRoomID)
	if fmt.Sprint(details(t, eo)["referrers"]) != "["+id+"]" {
		t.Errorf("details %v", eo["details"])
	}
	e.ok("task", "delete", id)
	e.ok("interaction", "delete", dataRoomID)
}

func TestCorrectAMistakeFollowUpsAndInteractions(t *testing.T) {
	e := taskWorld(t)
	desc := filepath.Join(e.Home, "d.md")
	must(t, os.WriteFile(desc, []byte("Remind Elena.\n"), 0o600))
	// Holder A's agent records an email and its follow-up.
	e.ok(as(acmeAgent, record("Data room documentation", "--ref", msgRef)...)...)
	id := idOf(t, e.ok(as(acmeAgent, mkTask("Follow up", "--interaction", dataRoomID, "--agent", acmeAgent, "--description-file", desc)...)...))
	itKey := storage.Key{Kind: "interaction-ref", Value: "interaction|" + orgID + "|" + msgRef}.Path()
	taskJSON := taskFile(t, id)
	for _, p := range []string{dataRoom, itKey, taskJSON, strings.TrimSuffix(taskJSON, ".json") + ".md", originKey(dataRoomID, "follow-up")} {
		if !exists(p) {
			t.Fatalf("%s is missing", p)
		}
	}
	// The interaction cannot go while the task refers to it.
	eo := e.refused("REFERENCED", 4, as(acmeAgent, "interaction", "delete", dataRoomID)...)
	if fmt.Sprint(details(t, eo)["referrers"]) != "["+id+"]" {
		t.Errorf("details %v", eo["details"])
	}
	// The task goes first, then the interaction, leaving nothing behind.
	outcomeOf(t, e.ok(as(acmeAgent, "task", "delete", id)...), "deleted")
	outcomeOf(t, e.ok(as(acmeAgent, "interaction", "delete", dataRoomID)...), "deleted")
	for _, p := range []string{dataRoom, itKey, taskJSON, strings.TrimSuffix(taskJSON, ".json") + ".md", originKey(dataRoomID, "follow-up")} {
		if exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	for f := range e.Files() {
		if strings.HasPrefix(f, "ws:records/tasks/") || strings.HasPrefix(f, "ws:records/interactions/") {
			t.Errorf("leftover %s", f)
		}
	}
	// Holder B's agent records the same email and gets a new interaction of its own holder.
	d := outcomeOf(t, e.ok(as(otherAgent, record("Data room documentation", "--ref", msgRef)...)...), "created")
	if d["holder"] != otherOrg {
		t.Errorf("holder %v", d["holder"])
	}
	// And then its own follow-up.
	t2 := outcomeOf(t, e.ok(as(otherAgent, "task", "create", "--title", "Follow up", "--assignee", otherHuman, "--interaction", d["id"].(string))...), "created")
	if t2["holder"] != otherOrg {
		t.Errorf("task holder %v", t2["holder"])
	}
}

func TestCheckReportsTaskOriginsAndLinks(t *testing.T) {
	e := taskWorld(t)
	e.ok(record("Data room documentation")...)
	id := idOf(t, e.ok(mkTask("Follow up", "--interaction", dataRoomID, "--agent", acmeAgent)...))
	other := idOf(t, e.ok(as(otherAgent, record("Other holder's note")...)...))
	if got := e.findings(); len(got) != 0 {
		t.Fatalf("clean: %v", got)
	}
	path := taskFile(t, id)
	rec := readRecord(t, path)

	// A key deleted by hand.
	must(t, os.Remove(originKey(dataRoomID, "follow-up")))
	got := e.findings()
	if r := strings.Join(got["VALUE_WITHOUT_KEY"], "\n"); !strings.Contains(r, path) || !strings.Contains(r, "task-origin") {
		t.Errorf("missing key: %v", got)
	}
	// A stale key: it points at a task that is not there, or one that does not have it.
	stale := originKey(dataRoomID, "ping")
	must(t, os.MkdirAll(filepath.Dir(stale), 0o700))
	raw, _ := json.Marshal(map[string]string{"key": "task|" + dataRoomID + "|ping", "record": "tk_01m87p0g67f8yshxy3v5a56vmk"})
	must(t, os.WriteFile(stale, raw, 0o600))
	got = e.findings()
	if r := strings.Join(got["KEY_WITHOUT_VALUE"], "\n"); !strings.Contains(r, stale) {
		t.Errorf("stale key: %v", got)
	}
	raw, _ = json.Marshal(map[string]string{"key": "task|" + dataRoomID + "|ping", "record": id})
	must(t, os.WriteFile(stale, raw, 0o600))
	if r := strings.Join(e.findings()["KEY_WITHOUT_VALUE"], "\n"); !strings.Contains(r, "does not have it") {
		t.Errorf("stale key on a real task: %v", r)
	}
	must(t, os.Remove(stale))
	// A duplicated origin: a second task with the same interaction and purpose.
	dup := idOf(t, e.ok(mkTask("Duplicate")...))
	dupRec := readRecord(t, taskFile(t, dup))
	dupRec["from"], dupRec["purpose"] = dataRoomID, "follow-up"
	writeRecord(t, taskFile(t, dup), dupRec)
	// (the key of the first was deleted above, so restore it to see the duplicate)
	raw, _ = json.Marshal(map[string]string{"key": "task|" + dataRoomID + "|follow-up", "record": id})
	must(t, os.MkdirAll(filepath.Dir(originKey(dataRoomID, "follow-up")), 0o700))
	must(t, os.WriteFile(originKey(dataRoomID, "follow-up"), raw, 0o600))
	got = e.findings()
	if r := strings.Join(got["VALUE_WITHOUT_KEY"], "\n"); !strings.Contains(r, taskFile(t, dup)) || !strings.Contains(r, "belongs to "+id) {
		t.Errorf("duplicate: %v", got)
	}
	must(t, os.Remove(taskFile(t, dup)))
	if got := e.findings(); len(got) != 0 {
		t.Fatalf("clean again: %v", got)
	}

	// A task whose origin is another holder's interaction.
	rec["from"] = other
	writeRecord(t, path, rec)
	before := e.Files()
	got = e.findings()
	if r := strings.Join(got["INVALID_RECORD"], "\n"); !strings.Contains(r, "belongs to "+otherOrg) {
		t.Errorf("origin of another holder: %v", got)
	}
	if fmt.Sprint(before) != fmt.Sprint(e.Files()) {
		t.Error("check changed a file")
	}
	// Missing records and people of another holder.
	rec["from"], rec["assignee"], rec["agent"] = "it_2026-01-01-nothing", otherHuman, otherAgent
	writeRecord(t, path, rec)
	got = e.findings()
	if r := strings.Join(got["DANGLING_REFERENCE"], "\n"); !strings.Contains(r, "the origin it_2026-01-01-nothing does not exist") {
		t.Errorf("dangling: %v", got)
	}
	if r := strings.Join(got["INVALID_RECORD"], "\n"); !strings.Contains(r, "the assignee "+otherHuman+" works for") || !strings.Contains(r, "the agent "+otherAgent+" works for") {
		t.Errorf("people of another holder: %v", got)
	}
	rec["from"], rec["assignee"], rec["agent"] = dataRoomID, "us_nobody", "us_nobody-bot"
	writeRecord(t, path, rec)
	if r := strings.Join(e.findings()["DANGLING_REFERENCE"], "\n"); !strings.Contains(r, "the assignee us_nobody does not exist") || !strings.Contains(r, "the agent us_nobody-bot does not exist") {
		t.Errorf("dangling people: %v", r)
	}
	rec["assignee"], rec["agent"] = acmeAgent, evaID // swapped kinds
	writeRecord(t, path, rec)
	if r := strings.Join(e.findings()["INVALID_RECORD"], "\n"); !strings.Contains(r, "is an agent, and a task is assigned to a human") || !strings.Contains(r, "is a human") {
		t.Errorf("kinds: %v", r)
	}
	rec["assignee"], rec["agent"] = userID, bot2ID // an agent of someone else
	writeRecord(t, path, rec)
	if r := strings.Join(e.findings()["INVALID_RECORD"], "\n"); !strings.Contains(r, "not by the assignee") {
		t.Errorf("handler: %v", r)
	}
	rec["agent"] = acmeAgent
	rec["holder"] = "org_nowhere"
	writeRecord(t, path, rec)
	if len(e.findings()["DANGLING_REFERENCE"]) == 0 {
		t.Error("a holder that does not exist")
	}
	rec["holder"] = "org_norte-example"
	writeRecord(t, path, rec)
	if r := strings.Join(e.findings()["INVALID_RECORD"], "\n"); !strings.Contains(r, "not an own organization") {
		t.Errorf("holder: %v", r)
	}
	// Rules inside a record.
	rec["holder"] = orgID
	rec["title"], rec["due"], rec["purpose"] = " ", "soon", ""
	writeRecord(t, path, rec)
	if r := strings.Join(e.findings()["INVALID_RECORD"], "\n"); !strings.Contains(r, "needs a title") || !strings.Contains(r, "not a date") || !strings.Contains(r, "needs a purpose") {
		t.Errorf("rules: %v", r)
	}
}

func TestWORK_INV_03_AFinishedTaskCannotBeReopenedForAnInactiveAssignee(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok("task", "create", "--title", "Finish me", "--assignee", evaID, "--agent", bot2ID))
	e.ok("task", "status", id, "done")
	e.ok("user", "update", bot2ID, "--live=false")
	e.ok("user", "update", evaID, "--live=false")
	eo := e.refused("INVALID_VALUE", 2, "task", "status", id, "todo")
	if details(t, eo)["field"] != "assignee" {
		t.Errorf("details %v", eo["details"])
	}
	// Moving between statuses that are not live is fine, and so is reopening once the people are back.
	outcomeOf(t, e.ok("task", "status", id, "canceled"), "updated")
	e.ok("user", "update", evaID, "--live=true")
	eo = e.refused("INVALID_VALUE", 2, "task", "status", id, "todo") // the agent is still off
	if details(t, eo)["field"] != "agent" {
		t.Errorf("details %v", eo["details"])
	}
	e.ok("user", "update", bot2ID, "--live=true")
	outcomeOf(t, e.ok("task", "status", id, "todo"), "updated")
	// A live task whose people are not live is reported by check, whatever made it so.
	for _, user := range []string{"users/us_bot2-acme.json", "users/us_eva-acme.json"} {
		path := "records/" + user
		rec := readRecord(t, path)
		rec["live"] = false
		writeRecord(t, path, rec)
	}
	got := strings.Join(e.findings()["INVALID_RECORD"], "\n")
	if !strings.Contains(got, "the assignee "+evaID+" is not live") || !strings.Contains(got, "the agent "+bot2ID+" is not live") {
		t.Errorf("findings %v", got)
	}
}

func TestINTERACTIONS_AC_01_RepeatingAFollowUpIsSafeAfterTheWorldMovedOn(t *testing.T) {
	e := taskWorld(t)
	e.ok(record("Data room documentation")...)
	args := []string{"task", "create", "--title", "Chase", "--assignee", evaID, "--agent", bot2ID, "--interaction", dataRoomID}
	id := idOf(t, e.ok(args...))
	e.ok("task", "status", id, "done")
	e.ok("user", "update", bot2ID, "--live=false")
	e.ok("user", "update", evaID, "--live=false")
	e.setPurposes("ping")
	e.setStatuses(`[{"name":"done","category":"done"}]`)
	// The assignee, the purpose and the initial status are gone, yet the repeat finds its task.
	if d := outcomeOf(t, e.ok(args...), "existing"); d["id"] != id {
		t.Errorf("existing %v", d["id"])
	}
	// A repeat that contradicts it is still a conflict.
	e.refused("DEDUP_CONFLICT", 4, "task", "create", "--title", "Other", "--assignee", evaID, "--interaction", dataRoomID)
}

func TestWORKSPACE_INV_06_AnUpdateRecomputesCategoryAndLiveFromTheCatalog(t *testing.T) {
	e := taskWorld(t)
	id := idOf(t, e.ok(mkTask("Catalog will move")...))
	// The catalog now puts todo in done: the next edit stores the new category and live flag.
	e.setStatuses(`[{"name":"todo","category":"done"},{"name":"in-progress","category":"open"}]`)
	d := outcomeOf(t, e.ok("task", "update", id, "--title", "Moved"), "updated")
	if d["category"] != "done" || d["live"] != false {
		t.Errorf("updated %v", d)
	}
	if l := e.ok("task", "list"); l["total"] != float64(0) {
		t.Errorf("live list %v", l["total"])
	}
	if got := e.findings(); len(got) != 0 {
		t.Errorf("findings %v", got)
	}
	// And the other way round: becoming live again needs a live assignee.
	e.setStatuses(`[{"name":"todo","category":"open"}]`)
	e.ok("user", "create", "zed-acme", "--human", "--holder", orgID)
	id2 := idOf(t, e.ok("task", "create", "--title", "Zed's", "--assignee", "us_zed-acme"))
	e.setStatuses(`[{"name":"todo","category":"done"}]`)
	e.ok("task", "update", id2, "--title", "Zed's, done")
	e.ok("user", "update", "us_zed-acme", "--live=false")
	e.setStatuses(`[{"name":"todo","category":"open"}]`)
	e.refused("INVALID_VALUE", 2, "task", "update", id2, "--title", "Zed's again")
}

func TestTaskCreateRefusesARecordThatBreaksItsRules(t *testing.T) {
	e := taskWorld(t)
	e.setPurposes("Follow Up", "ping") // a catalog value that is not a slug
	eo := e.refused("INVALID_VALUE", 2, mkTask("x", "--purpose", "Follow Up")...)
	if !strings.Contains(eo["message"].(string), "purpose") {
		t.Errorf("message %v", eo["message"])
	}
	outcomeOf(t, e.ok(mkTask("x", "--purpose", "ping")...), "created")
}

func TestWORKSPACE_AC_10_AMalformedReferrerStopsADeleteUntilRepaired(t *testing.T) {
	e := taskWorld(t)
	tom := tomID(t, e)
	id := idOf(t, e.ok(mkTask("Send the teaser", "--assignee", tom, "--due", "2026-12-01")...))
	path := taskFile(t, id)
	good, err := os.ReadFile(path)
	must(t, err)
	var rec map[string]any
	must(t, json.Unmarshal(good, &rec))
	rec["due"] = []string{"2026-12-01"}
	bad, err := json.Marshal(rec)
	must(t, err)
	must(t, os.WriteFile(path, bad, 0o600))

	// The guard cannot tell what the task refers to: it fails closed, with the path.
	eo := e.refused("STORAGE_UNAVAILABLE", 5, "user", "delete", tom)
	if !strings.Contains(fmt.Sprint(eo["message"]), path) || !strings.Contains(fmt.Sprint(eo["hint"]), "kravis check") {
		t.Errorf("error %v", eo)
	}
	// Deactivation fails closed the same way.
	e.refused("STORAGE_UNAVAILABLE", 5, "user", "update", tom, "--live=false")

	must(t, os.WriteFile(path, good, 0o600))
	eo = e.refused("REFERENCED", 4, "user", "delete", tom)
	if fmt.Sprint(details(t, eo)["referrers"]) != "["+id+"]" {
		t.Errorf("details %v", eo["details"])
	}
	e.ok("task", "delete", id)
	outcomeOf(t, e.ok("user", "delete", tom), "deleted")
}
