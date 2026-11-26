package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// searchWorld is a noteWorld with a few records to search: holder A's
// deal dl_grupo-sierra and project pr_data-room, Elena Ruiz and Marco Silva.
func searchWorld(t *testing.T) *env {
	t.Helper()
	return noteWorld(t)
}

func hitsOf(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	var hs []map[string]any
	for _, h := range out["data"].([]any) {
		hs = append(hs, h.(map[string]any))
	}
	return hs
}

// found runs a search and returns the IDs of its hits, in order.
func (e *env) found(args ...string) []string {
	e.t.Helper()
	return ids(e.t, e.ok(append([]string{"search"}, args...)...))
}

// fieldsOf lists the fields a hit matched, in order.
func fieldsOf(h map[string]any) []string {
	var out []string
	for _, m := range h["matches"].([]any) {
		out = append(out, m.(map[string]any)["field"].(string))
	}
	return out
}

func excerptOf(h map[string]any, field string) string {
	for _, m := range h["matches"].([]any) {
		if m := m.(map[string]any); m["field"] == field {
			return m["excerpt"].(string)
		}
	}
	return "<no such match>"
}

func TestWORKSPACE_AC_14_EveryWordMustAppearInAnyOrderAndAnyField(t *testing.T) {
	e := searchWorld(t)
	id := data(t, e.runOK("The CFO asked for the index.\n", append(record("Data room documentation", "--ref", msgRef), "--summary-file", "-")...))["id"].(string)
	// Two words in different fields (subject and summary), in any order.
	out := e.ok("search", "index", "DATA")
	if got := ids(t, out); !slices.Equal(got, []string{id}) {
		t.Fatalf("hits %v", got)
	}
	if f := fieldsOf(hitsOf(t, out)[0]); !slices.Equal(f, []string{"subject", "summary"}) {
		t.Errorf("matched fields %v", f)
	}
	// The text may come as one argument: it is split on whitespace.
	if got := e.found("  cfo \t room "); !slices.Equal(got, []string{id}) {
		t.Errorf("one argument: %v", got)
	}
	// A word that is nowhere makes the record a miss, and a miss is not an error.
	out = e.ok("search", "index", "zebra")
	if len(hitsOf(t, out)) != 0 || out["total"].(float64) != 0 || out["next_cursor"] != nil {
		t.Errorf("a miss: %v", out)
	}
	if _, ok := out["data"].([]any); !ok {
		t.Errorf("data of a miss is %v, want []", out["data"])
	}
}

func TestWORKSPACE_AC_14_WordsAreSubstringsWithoutCaseOrAccents(t *testing.T) {
	e := searchWorld(t)
	e.ok("person", "create", "--name", "José Peña", "--email", "j.pena@example.com")
	for _, text := range []string{"PEÑA", "pena", "jose", "NA@EX", "j.pe", "Peña josé"} {
		if got := e.found(text); !slices.Equal(got, []string{"pe_jose-pena"}) {
			t.Errorf("%q: %v", text, got)
		}
	}
	// Other characters are kept as they are, and there are no wildcards.
	for _, text := range []string{"pe*a", "pe-na", "jose.pena"} {
		if got := e.found(text); len(got) != 0 {
			t.Errorf("%q: %v", text, got)
		}
	}
	// Letters that do not decompose fold as slugs do.
	e.ok("person", "create", "--name", "Åsa Strauß")
	if got := e.found("STRAUSS asa"); !slices.Equal(got, []string{"pe_asa-strauss"}) {
		t.Errorf("ß: %v", got)
	}
}

func TestWORKSPACE_AC_14_EachSearchedFieldOfEachTypeIsSearched(t *testing.T) {
	e := searchWorld(t)
	e.ok("person", "create", "--name", "Quillon Vask", "--email", "zq.contact@example.com")
	e.ok("organization", "create", "--name", "Tarnby Works", "--domain", "orlop.example.com")
	e.ok(record("Wexford audit", "--ref", msgRef, "--summary-file", textFile(t, "Plumbline checklist agreed.\n"))...)
	e.ok(mkTask("Yarrow call", "--description-file", textFile(t, "Bring the zinnia deck.\n"))...)
	e.ok("deal", "create", "--party", "org_norte-example", "--holder-role", "advisor", "--name", "Ostrava bridge", "--description-file", textFile(t, "Mulberry terms pending.\n"))
	e.ok("project", "create", "--name", "Quartz survey", "--description-file", textFile(t, "Nightjar fieldwork.\n"))
	e.ok(mkNote(t, "Heron wants a quieter venue.\n", elenaID)...)
	for _, tc := range []struct{ text, typ, field string }{
		{"quillon", "person", "name"},
		{"zq.contact", "person", "emails"},
		{"tarnby", "organization", "name"},
		{"orlop.example", "organization", "domains"},
		{"wexford", "interaction", "subject"},
		{"plumbline", "interaction", "summary"},
		{"yarrow", "task", "title"},
		{"zinnia", "task", "description"},
		{"ostrava", "deal", "name"},
		{"mulberry", "deal", "description"},
		{"quartz", "project", "name"},
		{"nightjar", "project", "description"},
		{"quieter", "note", "text"},
	} {
		hs := hitsOf(t, e.ok("search", tc.text))
		if len(hs) != 1 || hs[0]["type"] != tc.typ || !slices.Equal(fieldsOf(hs[0]), []string{tc.field}) {
			t.Errorf("%q: %v", tc.text, hs)
		}
	}
}

func TestWORKSPACE_AC_14_IDsLinksAndUsersAreNotSearched(t *testing.T) {
	e := searchWorld(t)
	e.ok(record("Data room documentation", "--ref", msgRef, "--deal", sierraDeal)...)
	e.ok(mkNote(t, "Likes small teams.\n", elenaID)...)
	// IDs are not text: a person's, a deal's, an interaction's, a user's.
	for _, text := range []string{"pe_elena-ruiz", "pe_", "dl_grupo", "it_2026", "us_ana", "org_acme", "nt_"} {
		if got := e.found(text); len(got) != 0 {
			t.Errorf("%q found %v", text, got)
		}
	}
	// A link is not text either: the interaction has Elena as a participant,
	// the note has her as a subject, and only her own record matches.
	if got := e.found("elena"); !slices.Equal(got, []string{elenaID}) {
		t.Errorf("elena: %v", got)
	}
	// Users are not searched, by name, and they are not a type.
	e.ok("user", "update", userID, "--name", "Ana Brightwater")
	if got := e.found("brightwater"); len(got) != 0 {
		t.Errorf("a user was found: %v", got)
	}
	// The other fields are not searched either: channel, deal stage.
	if got := e.found("email"); len(got) != 0 {
		t.Errorf("channel: %v", got)
	}
}

func TestWORKSPACE_AC_14_ScopeIsTheActorsHolderPlusTheDirectory(t *testing.T) {
	e := searchWorld(t)
	e.ok(mkTask("Zeta plan")...)
	e.ok(as(otherHuman, "task", "create", "--title", "Zeta plan", "--assignee", otherHuman)...)
	e.ok("person", "create", "--name", "Zeta Contact")
	// Holder A: its task and the shared person.
	out := e.ok("search", "zeta")
	if f := obj(t, out, "filters"); f["holder"] != orgID || f["live"] != true || f["text"] != "zeta" {
		t.Errorf("filters %v", f)
	}
	hs := hitsOf(t, out)
	if len(hs) != 2 {
		t.Fatalf("holder A: %v", hs)
	}
	for _, h := range hs {
		if h["type"] == "task" && h["holder"] != orgID {
			t.Errorf("a task of another holder: %v", h)
		}
	}
	// The other holder sees its own task and the same directory.
	out = e.ok(as(otherHuman, "search", "zeta")...)
	if obj(t, out, "filters")["holder"] != otherOrg || len(hitsOf(t, out)) != 2 {
		t.Errorf("holder B: %v", out)
	}
	// --all-holders lifts the limit on work records, and says so by leaving holder out.
	out = e.ok("search", "zeta", "--all-holders")
	if _, ok := obj(t, out, "filters")["holder"]; ok || len(hitsOf(t, out)) != 3 {
		t.Errorf("all holders: %v", out)
	}
	// Without an actor nothing is limited and filters has no holder.
	e.noActor()
	out = e.ok("search", "zeta")
	if _, ok := obj(t, out, "filters")["holder"]; ok || len(hitsOf(t, out)) != 3 {
		t.Errorf("no actor: %v", out)
	}
}

func TestWORKSPACE_AC_14_TypeNarrowsTheSearch(t *testing.T) {
	e := searchWorld(t)
	e.ok("person", "create", "--name", "Marlow Person")
	e.ok("organization", "create", "--name", "Marlow Works")
	e.ok(mkTask("Marlow task")...)
	if got := e.found("marlow"); len(got) != 3 {
		t.Errorf("all types: %v", got)
	}
	out := e.ok("search", "marlow", "--type", "task")
	if got := ids(t, out); len(got) != 1 || !strings.HasPrefix(got[0], "tk_") {
		t.Errorf("one type: %v", got)
	}
	if f := obj(t, out, "filters"); fmt.Sprint(f["type"]) != "[task]" {
		t.Errorf("filters %v", f)
	}
	// Repeatable: the echo is in the contract's order, whatever the order given.
	out = e.ok("search", "marlow", "--type", "task", "--type", "person", "--type", "task")
	if got := ids(t, out); len(got) != 2 {
		t.Errorf("two types: %v", got)
	}
	if f := obj(t, out, "filters"); fmt.Sprint(f["type"]) != "[person task]" {
		t.Errorf("filters %v", f)
	}
	// An unknown type, and user, are refused with the searchable ones.
	for _, bad := range []string{"user", "widget"} {
		eo := e.refused("INVALID_VALUE", 2, "search", "marlow", "--type", bad)
		d := eo["details"].(map[string]any)
		if d["field"] != "type" || fmt.Sprint(d["allowed"]) != "[person organization interaction task deal project note]" {
			t.Errorf("%s: details %v", bad, d)
		}
	}
}

func TestWORKSPACE_AC_14_AllIncludesRecordsThatAreNotLive(t *testing.T) {
	e := searchWorld(t)
	task := idOf(t, e.ok(mkTask("Finch review")...))
	e.ok("task", "status", task, "done")
	e.ok("organization", "create", "--name", "Finch Retired")
	e.ok("organization", "update", "org_finch-retired", "--live=false")
	e.ok(mkNote(t, "Finch likes tea.\n", elenaID)...)
	live := e.ok("search", "finch")
	if got := ids(t, live); len(got) != 1 || !strings.HasPrefix(got[0], "nt_") {
		t.Errorf("live only: %v", got)
	}
	if obj(t, live, "filters")["live"] != true {
		t.Errorf("filters %v", live["filters"])
	}
	all := e.ok("search", "finch", "--all")
	if got := ids(t, all); len(got) != 3 {
		t.Errorf("--all: %v", got)
	}
	if _, ok := obj(t, all, "filters")["live"]; ok {
		t.Errorf("--all lists live in filters: %v", all["filters"])
	}
	for _, h := range hitsOf(t, all) {
		if h["type"] == "task" && h["live"] != false {
			t.Errorf("task %v", h)
		}
	}
}

func TestWORKSPACE_AC_14_HitsComeNewestUpdateFirstWithTheIDBreakingTies(t *testing.T) {
	e := searchWorld(t)
	at := func(h int) { e.Now = time.Date(2026, 11, 24, h, 0, 0, 0, time.UTC) }
	at(9)
	e.ok("person", "create", "--name", "Sort Alpha")
	at(11)
	e.ok("person", "create", "--name", "Sort Charlie")
	e.ok("organization", "create", "--name", "Sort Delta")
	at(10)
	e.ok("person", "create", "--name", "Sort Bravo")
	// Same instant: the ID breaks the tie, across types.
	if got := e.found("sort"); !slices.Equal(got, []string{"org_sort-delta", "pe_sort-charlie", "pe_sort-bravo", "pe_sort-alpha"}) {
		t.Errorf("by creation: %v", got)
	}
	// An update moves a record to the front, and updated_at is shown.
	at(12)
	e.ok("person", "update", "pe_sort-alpha", "--name", "Sort Alpha Two")
	out := e.ok("search", "sort")
	if got := ids(t, out); got[0] != "pe_sort-alpha" {
		t.Errorf("after the update: %v", got)
	}
	if u := hitsOf(t, out)[0]["updated_at"]; u != "2026-11-24T12:00:00Z" {
		t.Errorf("updated_at %v", u)
	}
	// A record with no updated_at uses its created_at, in the hit and the order.
	at(13)
	e.ok("person", "update", "pe_sort-charlie", "--name", "Sort Charlie Two")
	for _, id := range []string{"pe_sort-bravo", "pe_sort-charlie"} { // bravo created at 10, charlie at 11
		path := "records/people/" + id + ".json"
		raw, err := os.ReadFile(path)
		must(t, err)
		kept := slices.DeleteFunc(strings.Split(string(raw), "\n"), func(l string) bool {
			return strings.Contains(l, `"updated_at"`) || strings.Contains(l, `"updated_by"`)
		})
		kept[len(kept)-3] = strings.TrimSuffix(kept[len(kept)-3], ",") // the last field takes no comma
		must(t, os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600))
	}
	out = e.ok("search", "sort")
	byID := map[string]string{}
	for _, h := range hitsOf(t, out) {
		byID[h["id"].(string)] = h["updated_at"].(string)
	}
	if byID["pe_sort-bravo"] != "2026-11-24T10:00:00Z" || byID["pe_sort-charlie"] != "2026-11-24T11:00:00Z" {
		t.Errorf("created_at stands in: %v", byID)
	}
	if got := ids(t, out); !slices.Equal(got, []string{"pe_sort-alpha", "org_sort-delta", "pe_sort-charlie", "pe_sort-bravo"}) {
		t.Errorf("order with no updated_at: %v", got)
	}
}

func TestWORKSPACE_AC_14_AHitHasTheCompactViewUpdatedAtAndMatches(t *testing.T) {
	e := searchWorld(t)
	out := e.ok("search", "ruiz", "--type", "person")
	h := hitsOf(t, out)[0]
	for k, want := range map[string]any{"type": "person", "id": elenaID, "name": "Elena Ruiz", "live": true, "updated_at": "2026-11-24T09:30:00Z"} {
		if h[k] != want {
			t.Errorf("%s = %v, want %v", k, h[k], want)
		}
	}
	if fmt.Sprint(h["emails"]) != "["+elenaEmail+"]" {
		t.Errorf("hit %v", h)
	}
	// The raw order: type first, then the compact view, then updated_at and matches.
	r := e.run("search", "ruiz", "--type", "person")
	if !strings.Contains(r.Stdout, `{"type":"person","id":"pe_elena-ruiz","name":"Elena Ruiz"`) ||
		!strings.Contains(r.Stdout, `"updated_at":`) || !strings.Contains(r.Stdout, `"matches":[{"field":"name","excerpt":"Elena Ruiz"},{"field":"emails","excerpt":"e.ruiz@example.com"}]}`) {
		t.Errorf("stdout %s", r.Stdout)
	}
	// Tasks and notes show the compact view as their lists do.
	e.ok(mkTask("Tidy the data room", "--project", "pr_data-room")...)
	task := hitsOf(t, e.ok("search", "tidy"))[0]
	if task["project"] != "pr_data-room" || task["deal"] != sierraDeal {
		t.Errorf("task hit %v", task)
	}
	e.ok(mkNote(t, "First line\nSecond line about lanterns.\n", elenaID)...)
	note := hitsOf(t, e.ok("search", "lanterns"))[0]
	if note["excerpt"] != "First line" || note["type"] != "note" {
		t.Errorf("note hit %v", note)
	}
}

func TestWORKSPACE_AC_14_MatchesGiveTheFieldAndAnExcerpt(t *testing.T) {
	e := searchWorld(t)
	// Several emails: the first, in stored order, that holds a word.
	e.ok("person", "create", "--name", "Multi Mail", "--email", "b.second@example.com", "--email", "a.first@example.com", "--email", "c.third@example.com")
	h := hitsOf(t, e.ok("search", "example.com", "multi"))[0]
	if excerptOf(h, "emails") != "a.first@example.com" || excerptOf(h, "name") != "Multi Mail" {
		t.Errorf("emails: %v", h["matches"])
	}
	h = hitsOf(t, e.ok("search", "third", "multi"))[0]
	if excerptOf(h, "emails") != "c.third@example.com" {
		t.Errorf("emails: %v", h["matches"])
	}
	e.ok("organization", "create", "--name", "Doms", "--domain", "zzz.example.com", "--domain", "aaa.example.com")
	h = hitsOf(t, e.ok("search", "aaa"))[0]
	if excerptOf(h, "domains") != "aaa.example.com" {
		t.Errorf("domains: %v", h["matches"])
	}
	// A short text is shown whole, even when it holds the word far inside.
	e.ok(mkTask("A rather long task title that goes on and on, with the word pelican far inside it")...)
	h = hitsOf(t, e.ok("search", "pelican"))[0]
	if excerptOf(h, "title") != "A rather long task title that goes on and on, with the word pelican far inside it" {
		t.Errorf("title: %v", h["matches"])
	}
	// A long text gives a window of 80 characters around the first match, cut with …
	long := strings.Repeat("a", 100) + " MARMOT " + strings.Repeat("b", 100) + " marmot"
	e.ok(mkNote(t, long, elenaID)...)
	h = hitsOf(t, e.ok("search", "marmot"))[0]
	ex := excerptOf(h, "text")
	if !strings.HasPrefix(ex, "…") || !strings.HasSuffix(ex, "…") || !strings.Contains(ex, " MARMOT ") ||
		len([]rune(strings.Trim(ex, "…"))) != 80 || strings.Contains(ex, "marmot") {
		t.Errorf("window %q (%d)", ex, len([]rune(ex)))
	}
	// A match near the start is cut only at the end, near the end only at the start.
	e.ok(mkNote(t, "Ibis at the start "+strings.Repeat("x", 200), elenaID)...)
	ex = excerptOf(hitsOf(t, e.ok("search", "ibis"))[0], "text")
	if strings.HasPrefix(ex, "…") || !strings.HasSuffix(ex, "…") || !strings.HasPrefix(ex, "Ibis at") {
		t.Errorf("start %q", ex)
	}
	e.ok(mkNote(t, strings.Repeat("x", 200)+" at the end: koala", elenaID)...)
	ex = excerptOf(hitsOf(t, e.ok("search", "koala"))[0], "text")
	if !strings.HasPrefix(ex, "…") || strings.HasSuffix(ex, "…") || !strings.HasSuffix(ex, "end: koala") {
		t.Errorf("end %q", ex)
	}
	// A long text of at most 80 characters is shown whole, accents as written.
	e.ok(mkNote(t, "Reunión en Málaga con el CFO", elenaID)...)
	if ex = excerptOf(hitsOf(t, e.ok("search", "malaga"))[0], "text"); ex != "Reunión en Málaga con el CFO" {
		t.Errorf("short long text %q", ex)
	}
	// The window cuts the original text even where folding changed its length.
	e.ok(mkNote(t, strings.Repeat("ß", 90)+" Ñandú "+strings.Repeat("ß", 90), elenaID)...)
	ex = excerptOf(hitsOf(t, e.ok("search", "nandu"))[0], "text")
	if !strings.Contains(ex, " Ñandú ") || len([]rune(strings.Trim(ex, "…"))) != 80 {
		t.Errorf("accents %q", ex)
	}
}

func TestWORKSPACE_AC_14_SearchPagesLikeAList(t *testing.T) {
	e := searchWorld(t)
	for i := range 5 {
		e.Now = time.Date(2026, 11, 24, 9+i, 0, 0, 0, time.UTC)
		e.ok("person", "create", "--name", fmt.Sprintf("Page Person %d", i))
	}
	e.Now = time.Time{}
	var got []string
	cursor := ""
	for pages := 0; ; pages++ {
		args := []string{"search", "page", "--limit", "2"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		out := e.ok(args...)
		got = append(got, ids(t, out)...)
		if out["total"].(float64) != 5 {
			t.Errorf("total %v", out["total"])
		}
		if out["next_cursor"] == nil {
			if pages != 2 || len(hitsOf(t, out)) != 1 {
				t.Errorf("last page after %d pages: %v", pages, out)
			}
			break
		}
		cursor = out["next_cursor"].(string)
		if pages > 5 {
			t.Fatal("never ends")
		}
	}
	want := []string{"pe_page-person-4", "pe_page-person-3", "pe_page-person-2", "pe_page-person-1", "pe_page-person-0"}
	if !slices.Equal(got, want) {
		t.Errorf("pages %v, want %v", got, want)
	}
	if all := e.found("page", "--limit", "0"); !slices.Equal(all, want) {
		t.Errorf("--limit 0: %v", all)
	}
	// A cursor belongs to its text, types and --all; the others are refused as in lists.
	first := e.ok("search", "page", "--limit", "2")["next_cursor"].(string)
	for _, args := range [][]string{
		{"search", "person", "--limit", "2", "--cursor", first},
		{"search", "page", "--limit", "2", "--all", "--cursor", first},
		{"search", "page", "--limit", "2", "--type", "person", "--cursor", first},
		{"search", "page", "--limit", "2", "--cursor", "nonsense"},
		{"search", "page", "--limit", "0", "--cursor", first},
		{"search", "page", "--limit", "-1"},
		{"search", "page", "--limit", "x"},
	} {
		e.refused("INVALID_VALUE", 2, args...)
	}
	// The same search continues.
	if again := ids(t, e.ok("search", "page", "--limit", "2", "--cursor", first)); !slices.Equal(again, want[2:4]) {
		t.Errorf("continued %v", again)
	}
}

func TestWORKSPACE_AC_14_TextWithNoWordsIsRefused(t *testing.T) {
	e := searchWorld(t)
	for _, args := range [][]string{{"search"}, {"search", ""}, {"search", "   "}, {"search", " ", "\t"}} {
		d := e.refused("INVALID_VALUE", 2, args...)["details"].(map[string]any)
		if d["field"] != "text" {
			t.Errorf("%v: details %v", args, d)
		}
	}
}

func TestWORKSPACE_AC_14_SearchOnlyReads(t *testing.T) {
	e := searchWorld(t)
	e.ok(mkNote(t, "Nothing changes.\n", elenaID)...)
	before := e.Files()
	e.ok("search", "nothing")
	e.ok("search", "elena", "--all")
	if after := e.Files(); len(after) != len(before) {
		t.Errorf("files before %d, after %d", len(before), len(after))
	}
	for k, v := range before {
		if e.Files()[k] != v {
			t.Errorf("%s changed", k)
		}
	}
}
