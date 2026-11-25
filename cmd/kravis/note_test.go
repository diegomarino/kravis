package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// noteWorld is a projectWorld with holder A's project pr_data-room.
func noteWorld(t *testing.T) *env {
	t.Helper()
	e := projectWorld(t)
	e.ok(mkProject("--deal", sierraDeal)...)
	return e
}

// textFile writes text to a file outside the workspace and returns its path.
func textFile(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "text.md")
	must(t, os.WriteFile(p, []byte(text), 0o600))
	return p
}

// mkNote is the args of a note create about the subjects, with the text.
func mkNote(t *testing.T, text string, subjects ...string) []string {
	t.Helper()
	args := []string{"note", "create", "--text-file", textFile(t, text)}
	for _, s := range subjects {
		args = append(args, "--subject", s)
	}
	return args
}

// noteFile is where a note is stored: its JSON path, in a month folder.
func noteFile(t *testing.T, id string) string {
	t.Helper()
	m, err := filepath.Glob("records/notes/*/" + id + ".json")
	if err != nil || len(m) != 1 {
		t.Fatalf("note file of %s: %v %v", id, m, err)
	}
	if !regexp.MustCompile(`^records/notes/\d{4}-\d{2}/nt_[0-7][0-9a-z]{25}\.json$`).MatchString(filepath.ToSlash(m[0])) {
		t.Errorf("note path %s is not in a month folder", m[0])
	}
	return filepath.ToSlash(m[0])
}

func sidecar(path string) string { return strings.TrimSuffix(path, ".json") + ".md" }

func TestINTERACTIONS_INV_10_ANoteHasSubjectsAndTheHolderOfItsActor(t *testing.T) {
	e := noteWorld(t)
	out := e.ok(mkNote(t, "Met at the fair.\nLikes small teams.\n", elenaID, sierraDeal, "e.ruiz@example.com")...)
	d := outcomeOf(t, out, "created")
	id := d["id"].(string)
	if !strings.HasPrefix(id, "nt_") || d["holder"] != orgID || d["excerpt"] != "Met at the fair." {
		t.Errorf("created %v", d)
	}
	if s := fmt.Sprint(d["subjects"]); s != "[dl_grupo-sierra pe_elena-ruiz]" {
		t.Errorf("subjects %s", s)
	}
	path := noteFile(t, id)
	rec := readRecord(t, path)
	if rec["holder"] != orgID || rec["live"] != true || len(rec["subjects"].([]any)) != 2 {
		t.Errorf("stored %v", rec)
	}
	if _, has := rec["text"]; has {
		t.Error("the text is in the JSON")
	}
	if raw, err := os.ReadFile(sidecar(path)); err != nil || string(raw) != "Met at the fair.\nLikes small teams.\n" {
		t.Errorf("sidecar %q %v", raw, err)
	}
	// A note about a project and an organization, from the other holder.
	other := e.ok(as(otherAgent, append(mkNote(t, "Theirs", sierraID), "--subject", elenaID)...)...)
	if data(t, other)["holder"] != otherOrg {
		t.Errorf("other %v", other)
	}

	// No subject, no text, a blank text and what is not a subject.
	eo := e.refused("INVALID_VALUE", 2, "note", "create", "--text-file", textFile(t, "x"))
	if details(t, eo)["field"] != "subject" {
		t.Errorf("no subject %v", eo)
	}
	e.refused("INVALID_VALUE", 2, "note", "create", "--subject", elenaID)
	eo = e.refused("INVALID_VALUE", 2, mkNote(t, " \n", elenaID)...)
	if details(t, eo)["field"] != "text" {
		t.Errorf("blank text %v", eo)
	}
	eo = e.refused("INVALID_VALUE", 2, mkNote(t, "x", evaID)...)
	if details(t, eo)["field"] != "subject" {
		t.Errorf("a user %v", eo)
	}
	e.refused("NOT_FOUND", 3, mkNote(t, "x", "pe_ghost")...)
	e.refused("NOT_FOUND", 3, mkNote(t, "x", elenaID, "dl_ghost")...)
	// Another holder's deal or project is refused.
	eo = e.refused("HOLDER_MISMATCH", 2, mkNote(t, "About B's deal", otherDeal)...)
	if details(t, eo)["field"] != "subject" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("HOLDER_MISMATCH", 2, mkNote(t, "x", elenaID, otherDeal)...)
	e.ok(as(otherAgent, "project", "create", "--name", "Theirs")...)
	e.refused("HOLDER_MISMATCH", 2, mkNote(t, "x", "pr_theirs")...)
	// An update keeps the rule.
	e.refused("HOLDER_MISMATCH", 2, "note", "update", id, "--subject", otherDeal)
	e.refused("INVALID_VALUE", 2, "note", "update", id, "--subject", evaID)
	e.refused("NOT_FOUND", 3, "note", "update", id, "--subject", "pe_ghost")
}

func TestWORKSPACE_AC_04_NoteWritesReturnTheirOutcome(t *testing.T) {
	e := noteWorld(t)
	d := outcomeOf(t, e.runOK("Call notes\n", "note", "create", "--subject", elenaID, "--text-file", "-"), "created")
	id := d["id"].(string)
	if v := readRecord(t, noteFile(t, id))["version"]; v != float64(1) {
		t.Errorf("version %v", v)
	}
	if strings.Join(keysOf(d), ",") != "created_at,excerpt,holder,id,live,subjects,text" {
		t.Errorf("compact %v", keysOf(d))
	}
	outcomeOf(t, e.runOK("New text\n", "note", "update", id, "--text-file", "-"), "updated")
	if u := outcomeOf(t, e.runOK("New text\n", "note", "update", id, "--text-file", "-"), "unchanged"); u["text"] != nil {
		t.Errorf("an unchanged update shows the text: %v", u)
	}
	if v := readRecord(t, noteFile(t, id))["version"]; v != float64(2) {
		t.Errorf("version %v", v)
	}
	if raw, _ := os.ReadFile(sidecar(noteFile(t, id))); string(raw) != "New text\n" {
		t.Errorf("sidecar %q", raw)
	}
	outcomeOf(t, e.ok("note", "update", id, "--subject", sierraDeal, "--subject", "pr_data-room"), "updated")
	outcomeOf(t, e.ok("note", "update", id, "--subject", sierraDeal), "unchanged")
	outcomeOf(t, e.ok("note", "update", id, "--remove-subject", "pr_data-room", "--remove-subject", "pr_data-room"), "updated")
	outcomeOf(t, e.ok("note", "update", id, "--remove-subject", sierraDeal), "updated")
	// Never below one subject, and never without text.
	eo := e.refused("INVALID_VALUE", 2, "note", "update", id, "--remove-subject", elenaID)
	if details(t, eo)["field"] != "remove-subject" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("INVALID_VALUE", 2, "note", "update", id, "--subject", sierraDeal, "--remove-subject", elenaID, "--remove-subject", sierraDeal)
	e.refused("INVALID_VALUE", 2, "note", "update", id, "--text-file", textFile(t, "\n"))
	e.refused("UNKNOWN_FIELD", 2, "note", "update", id, "--live=false")
	// get is compact; --full adds the text; the excerpt is cut at 80.
	if g := data(t, e.ok("note", "get", id)); g["excerpt"] != "New text" || g["text"] != nil {
		t.Errorf("get %v", g)
	}
	if g := data(t, e.ok("note", "get", id, "--full")); g["text"] != "New text\n" || g["version"] != float64(5) {
		t.Errorf("full %v", g)
	}
	long := strings.Repeat("é", 100)
	lid := idOf(t, e.ok(mkNote(t, "\n"+long+"\nmore", elenaID)...))
	if g := data(t, e.ok("note", "get", lid)); g["excerpt"] != strings.Repeat("é", 80) {
		t.Errorf("excerpt %v", g["excerpt"])
	}
	// Another holder's note does not exist for an actor.
	e.refused("NOT_FOUND", 3, as(otherAgent, "note", "get", id)...)
	e.refused("NOT_FOUND", 3, as(otherAgent, "note", "update", id, "--text-file", textFile(t, "x"))...)
	e.refused("NOT_FOUND", 3, as(otherAgent, "note", "delete", id)...)
	if g := data(t, e.ok("note", "get", id, "--all-holders")); g["id"] != id {
		t.Errorf("all holders %v", g)
	}
	// delete takes the note, its sidecar and nothing else.
	path := noteFile(t, id)
	d = outcomeOf(t, e.ok("note", "delete", id, "--full"), "deleted")
	if d["text"] != "New text\n" || exists(path) || exists(sidecar(path)) {
		t.Errorf("deleted %v", d)
	}
	e.refused("NOT_FOUND", 3, "note", "get", id)
	e.refused("NOT_FOUND", 3, "note", "delete", id)
}

func TestWORKSPACE_AC_05_NotesListNewestFirstWithACursor(t *testing.T) {
	e := noteWorld(t)
	var made []string
	for n, text := range []string{"First", "Second", "Third"} {
		id := idOf(t, e.ok(mkNote(t, text, elenaID)...))
		path := noteFile(t, id)
		rec := readRecord(t, path)
		rec["created_at"] = fmt.Sprintf("2026-11-2%dT10:00:00Z", n+1)
		writeRecord(t, path, rec)
		made = append(made, id)
	}
	e.ok(as(otherAgent, mkNote(t, "Theirs", sierraID)...)...)
	want := []string{made[2], made[1], made[0]}
	l := e.ok("note", "list")
	if got := ids(t, l); !slices.Equal(got, want) || l["total"] != float64(3) {
		t.Errorf("list %v %v", got, l["total"])
	}
	if r := l["data"].([]any)[0].(map[string]any); r["excerpt"] != "Third" || r["text"] != nil {
		t.Errorf("row %v", r)
	}
	p1 := e.ok("note", "list", "--limit", "2")
	p2 := e.ok("note", "list", "--limit", "2", "--cursor", p1["next_cursor"].(string))
	if got := append(ids(t, p1), ids(t, p2)...); !slices.Equal(got, want) || p2["next_cursor"] != nil {
		t.Errorf("pages %v", got)
	}
	if r := e.ok("note", "list", "--full")["data"].([]any)[2].(map[string]any); r["text"] != "First" {
		t.Errorf("full row %v", r)
	}
	if got := ids(t, e.ok("note", "list", "--all-holders")); len(got) != 4 {
		t.Errorf("all holders %v", got)
	}
	e.refused("INVALID_VALUE", 2, "note", "list", "--limit", "-1")
	e.refused("INVALID_VALUE", 2, "note", "list", "--cursor", "nope", "--limit", "2")
}

func TestWORKSPACE_AC_10_DeletingASubjectOfANoteIsRefusedAndListsTheNote(t *testing.T) {
	e := noteWorld(t)
	id := idOf(t, e.ok(mkNote(t, "About them all", elenaID, sierraID, sierraDeal, "pr_data-room")...))
	for _, c := range [][]string{{"person", "delete", elenaID}, {"organization", "delete", sierraID},
		{"deal", "delete", sierraDeal}, {"project", "delete", "pr_data-room"}} {
		eo := e.refused("REFERENCED", 4, c...)
		if !strings.Contains(fmt.Sprint(details(t, eo)["referrers"]), id) {
			t.Errorf("%v: referrers %v", c, eo["details"])
		}
	}
	// A holder with a note stays an own organization, and nothing refers to a note.
	e.ok(as(otherAgent, mkNote(t, "Theirs", sierraID)...)...)
	e.refused("REFERENCED", 4, "organization", "update", otherOrg, "--own=false")
	e.ok("note", "delete", id)
	e.ok("project", "delete", "pr_data-room")
	e.ok("deal", "delete", sierraDeal)
	// A person that only a note refers to is deleted once the note is.
	nora := idOf(t, e.ok("person", "create", "--name", "Nora Test", "--email", "nora@example.com"))
	nid := idOf(t, e.ok(mkNote(t, "About Nora", nora)...))
	e.refused("REFERENCED", 4, "person", "delete", nora)
	e.ok("note", "delete", nid)
	outcomeOf(t, e.ok("person", "delete", nora), "deleted")
}

func TestCheckReportsNotes(t *testing.T) {
	e := noteWorld(t)
	nora := idOf(t, e.ok("person", "create", "--name", "Nora Test"))
	id := idOf(t, e.ok(mkNote(t, "A note", nora, sierraDeal, "pr_data-room", sierraID)...))
	path := noteFile(t, id)
	if got := e.findings(); len(got) != 0 {
		t.Fatalf("clean workspace has findings %v", got)
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
	edit := func(change func(map[string]any)) {
		rec := readRecord(t, path)
		change(rec)
		writeRecord(t, path, rec)
	}
	// A subject file removed by hand.
	must(t, os.Remove("records/people/"+nora+".json"))
	has("DANGLING_REFERENCE", "the subject "+nora+" does not exist")
	edit(func(r map[string]any) {
		r["subjects"] = []string{sierraDeal, "dl_ghost", "pr_ghost", "pe_ghost", "org_ghost"}
	})
	for _, g := range []string{"dl_ghost", "pr_ghost", "pe_ghost", "org_ghost"} {
		has("DANGLING_REFERENCE", "the subject "+g+" does not exist")
	}
	// A deal of another holder.
	edit(func(r map[string]any) { r["subjects"] = []string{otherDeal} })
	has("HOLDER_MISMATCH", "the subject "+otherDeal)
	// A holder that does not exist, and one that is not own.
	edit(func(r map[string]any) { r["subjects"] = []string{sierraID}; r["holder"] = "org_ghost" })
	has("DANGLING_REFERENCE", "the holder org_ghost does not exist")
	edit(func(r map[string]any) { r["holder"] = sierraID })
	has("INVALID_RECORD", "not an own organization")
	// What breaks the note's rules.
	edit(func(r map[string]any) {
		r["holder"] = orgID
		r["subjects"] = []string{}
		r["live"] = false
	})
	has("INVALID_RECORD", "at least one subject")
	has("INVALID_RECORD", "always live")
	edit(func(r map[string]any) { r["subjects"] = []string{evaID, sierraID, sierraID}; r["live"] = true })
	has("INVALID_RECORD", "is not a person, organization, deal or project ID")
	has("INVALID_RECORD", "is repeated")
	edit(func(r map[string]any) { r["subjects"] = []string{sierraID} })
	// A note without its text.
	must(t, os.Remove(sidecar(path)))
	has("INVALID_RECORD", "the note has no text")
	must(t, os.WriteFile(sidecar(path), []byte("  \n"), 0o600))
	has("INVALID_RECORD", "the note has no text")
	must(t, os.WriteFile(sidecar(path), []byte("back"), 0o600))
	if got := e.findings(); len(got["INVALID_RECORD"])+len(got["DANGLING_REFERENCE"])+len(got["HOLDER_MISMATCH"]) != 0 {
		t.Errorf("repaired note still has findings %v", got)
	}
}

// A reader that fails the test if the command touches it.
type untouchedStdin struct{ t *testing.T }

func (u untouchedStdin) Read([]byte) (int, error) {
	u.t.Error("stdin was read before the actor was checked")
	return 0, io.EOF
}

func TestLongTextIsReadOnlyAfterTheActorIsResolved(t *testing.T) {
	e := world(t)
	var stdout, stderr bytes.Buffer
	code := runWith(e.deps(), []string{"note", "create", "--subject", orgID, "--text-file", "-", "--actor", "us_missing"},
		untouchedStdin{t}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "ACTOR_NOT_LIVE") {
		t.Errorf("code %d, stderr %q", code, stderr.String())
	}
	// With a live actor the text still comes from stdin.
	out := e.runWithStdin("From stdin.\n", "note", "create", "--subject", orgID, "--text-file", "-")
	if out.Code != 0 {
		t.Fatalf("code %d, stderr %q", out.Code, out.Stderr)
	}
}
