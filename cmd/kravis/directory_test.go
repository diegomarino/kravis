package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/diegomarino/kravis/internal/domain"
	"github.com/diegomarino/kravis/internal/storage"
)

// withMeta adds valid metadata to a record written by hand, so that check
// reports only what the test breaks.
func withMeta(rec string) []byte {
	return []byte(strings.TrimSuffix(rec, "}") + `,"version":1,"created_at":"2026-11-23T00:00:00Z","created_by":"us_ana-acme"}`)
}

func readRecord(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

const (
	elenaEmail = "e.ruiz@example.com"
	elenaID    = "pe_elena-ruiz"
)

func TestWORKSPACE_AC_04_EveryWriteReturnsAnOutcomeAndTheCompactView(t *testing.T) {
	e := newEnv(t)
	e.mustInit()

	d := outcomeOf(t, e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail), "created")
	if d["id"] != elenaID || d["live"] != true || fmt.Sprint(d["emails"]) != "["+elenaEmail+"]" {
		t.Errorf("created: %v", d)
	}

	// The same natural key: existing, nothing written.
	before := e.Files()
	outcomeOf(t, e.ok("person", "create", "--email", elenaEmail), "existing")
	if d := outcomeOf(t, e.ok("person", "create", "--email", "  E.Ruiz@Example.COM "), "existing"); d["id"] != elenaID {
		t.Errorf("an email with other case and spaces is the same: %v", d)
	}
	outcomeOf(t, e.ok("person", "create", "--name", "elena ruiz", "--email", elenaEmail), "existing")
	if !maps.Equal(before, e.Files()) {
		t.Error("existing wrote")
	}

	// Data that contradicts it is a conflict.
	eo := e.refused("DEDUP_CONFLICT", 4, "person", "create", "--name", "Otra Persona", "--email", elenaEmail)
	if det := obj(t, eo, "details"); det["id"] != elenaID || det["field"] != "name" {
		t.Errorf("details %v", det)
	}
	e.refused("DEDUP_CONFLICT", 4, "person", "create", "--email", elenaEmail, "--email", "new@example.com")

	// An update that changes something, then the same again.
	d = outcomeOf(t, e.ok("person", "update", elenaID, "--name", "Elena Ruiz Vega", "--fields", "version"), "updated")
	if d["name"] != "Elena Ruiz Vega" || d["version"] != float64(2) {
		t.Errorf("updated: %v", d)
	}
	before = e.Files()
	d = outcomeOf(t, e.ok("person", "update", elenaID, "--name", "Elena Ruiz Vega", "--fields", "version"), "unchanged")
	if d["version"] != float64(2) {
		t.Errorf("unchanged bumped the version: %v", d)
	}
	if !maps.Equal(before, e.Files()) {
		t.Error("unchanged wrote")
	}

	// Users: created, existing, conflict, updated, unchanged.
	d = outcomeOf(t, e.ok("user", "create", "bot-acme", "--agent", "--holder", orgID, "--handler", userID), "created")
	if d["id"] != "us_bot-acme" || d["kind"] != "agent" || d["holder"] != orgID || d["live"] != true {
		t.Errorf("user created: %v", d)
	}
	before = e.Files()
	outcomeOf(t, e.ok("user", "create", "bot-acme", "--agent", "--holder", orgID, "--handler", userID), "existing")
	if !maps.Equal(before, e.Files()) {
		t.Error("user existing wrote")
	}
	e.refused("DEDUP_CONFLICT", 4, "user", "create", "bot-acme", "--human", "--holder", orgID)
	d = outcomeOf(t, e.ok("user", "update", "us_bot-acme", "--name", "Bot Acme", "--fields", "version"), "updated")
	if d["name"] != "Bot Acme" || d["version"] != float64(2) {
		t.Errorf("user updated: %v", d)
	}
	before = e.Files()
	outcomeOf(t, e.ok("user", "update", "us_bot-acme", "--name", "Bot Acme"), "unchanged")
	if !maps.Equal(before, e.Files()) {
		t.Error("user unchanged wrote")
	}
	d = outcomeOf(t, e.ok("user", "delete", "us_bot-acme"), "deleted")
	if d["id"] != "us_bot-acme" {
		t.Errorf("user deleted: %v", d)
	}

	// Organizations: created, existing by domain, conflict.
	d = outcomeOf(t, e.ok("organization", "create", "--name", "Grupo Sierra", "--domain", "WWW.GrupoSierra.example.com"), "created")
	if d["id"] != "org_grupo-sierra" || fmt.Sprint(d["domains"]) != "[gruposierra.example.com]" || d["own"] != false {
		t.Errorf("organization: %v", d)
	}
	outcomeOf(t, e.ok("organization", "create", "--name", "Grupo Sierra", "--domain", "gruposierra.example.com"), "existing")
	e.refused("DEDUP_CONFLICT", 4, "organization", "create", "--name", "Sierra Holdings", "--domain", "gruposierra.example.com")
	e.refused("DEDUP_CONFLICT", 4, "organization", "create", "--name", "Grupo Sierra", "--own", "--domain", "gruposierra.example.com")
	outcomeOf(t, e.ok("organization", "update", "org_grupo-sierra", "--own"), "updated")
	outcomeOf(t, e.ok("organization", "update", "org_grupo-sierra", "--own"), "unchanged")

	// A delete returns the record as it was.
	d = outcomeOf(t, e.ok("person", "delete", elenaID), "deleted")
	if d["id"] != elenaID || d["name"] != "Elena Ruiz Vega" {
		t.Errorf("deleted: %v", d)
	}
	if _, err := os.Stat("records/people/" + elenaID + ".json"); !os.IsNotExist(err) {
		t.Error("the record is still there")
	}
	// Its email is free again.
	outcomeOf(t, e.ok("person", "create", "--email", elenaEmail), "created")
}

func TestWORKSPACE_AC_05_ListsPageInSlugOrderWithATotalAndACursor(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	var want []string
	for i := range 120 {
		// Accents and case do not move a name: slugs are compared.
		first := []string{"Álvaro", "bruno", "Çelik", "Dana"}[i%4]
		out := e.ok("person", "create", "--name", fmt.Sprintf("%s %03d", first, i))
		want = append(want, data(t, out)["id"].(string))
	}
	slices.SortFunc(want, func(a, b string) int { return strings.Compare(a, b) })

	page := e.ok("person", "list")
	if n := len(page["data"].([]any)); n != 50 || page["total"] != float64(120) {
		t.Fatalf("default page: %d records, total %v", n, page["total"])
	}

	var got []string
	cursor := ""
	for pages := 0; ; pages++ {
		args := []string{"person", "list", "--limit", "25"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		out := e.ok(args...)
		if out["total"] != float64(120) {
			t.Fatalf("total %v", out["total"])
		}
		for _, r := range out["data"].([]any) {
			got = append(got, r.(map[string]any)["id"].(string))
		}
		if pages == 1 {
			// Records created or deleted meanwhile do not move the others.
			e.ok("person", "create", "--name", "Aaron First")
			e.ok("person", "delete", got[0])
			want = want[1:]
		}
		next, isString := out["next_cursor"].(string)
		if !isString {
			if v, present := out["next_cursor"]; !present || v != nil {
				t.Fatalf("the last page needs next_cursor null: %v", out)
			}
			break
		}
		cursor = next
	}
	if !slices.Equal(got[1:], want) {
		t.Errorf("paged %d records, want %d, in slug order without gaps or repeats\ngot  %v\nwant %v", len(got)-1, len(want), got[1:], want)
	}

	// Users page the same way, by the name as a slug, the ID for a user without one.
	type named struct{ id, name string }
	wantNamed := []named{{userID, userName}, {"us_bot-acme", ""}}
	for i := range 7 {
		first := []string{"Álvaro", "bruno", "Çelik"}[i%3]
		n := named{fmt.Sprintf("us_u%02d-acme", i), fmt.Sprintf("%s %02d", first, i)}
		e.ok("user", "create", strings.TrimPrefix(n.id, "us_"), "--human", "--holder", orgID, "--name", n.name)
		wantNamed = append(wantNamed, n)
	}
	e.ok("user", "create", "bot-acme", "--agent", "--holder", orgID, "--handler", userID)
	slices.SortFunc(wantNamed, func(a, b named) int {
		return strings.Compare(domain.Slug(firstNonEmpty(a.name, a.id))+a.id, domain.Slug(firstNonEmpty(b.name, b.id))+b.id)
	})
	var wantUsers, gotUsers []string
	for _, n := range wantNamed {
		wantUsers = append(wantUsers, n.id)
	}
	userCursor := ""
	for {
		args := []string{"user", "list", "--limit", "3"}
		if userCursor != "" {
			args = append(args, "--cursor", userCursor)
		}
		out := e.ok(args...)
		if out["total"] != float64(9) {
			t.Fatalf("users total %v", out["total"])
		}
		for _, r := range out["data"].([]any) {
			gotUsers = append(gotUsers, r.(map[string]any)["id"].(string))
		}
		next, isString := out["next_cursor"].(string)
		if !isString {
			if v, present := out["next_cursor"]; !present || v != nil {
				t.Fatalf("the last page needs next_cursor null: %v", out)
			}
			break
		}
		userCursor = next
	}
	if !slices.Equal(gotUsers, wantUsers) {
		t.Errorf("users paged\ngot  %v\nwant %v", gotUsers, wantUsers)
	}

	all := e.ok("person", "list", "--limit", "0")
	if len(all["data"].([]any)) != 120 || all["next_cursor"] != nil {
		t.Errorf("--limit 0: %d records, next %v", len(all["data"].([]any)), all["next_cursor"])
	}

	two := e.ok("person", "list", "--limit", "2")
	c := two["next_cursor"].(string)
	for name, args := range map[string][]string{
		"negative limit":             {"person", "list", "--limit", "-1"},
		"non-numeric limit":          {"person", "list", "--limit", "many"},
		"limit 0 and cursor":         {"person", "list", "--limit", "0", "--cursor", c},
		"corrupt cursor":             {"person", "list", "--cursor", "not-a-cursor"},
		"other filters":              {"person", "list", "--all", "--cursor", c},
		"other type":                 {"organization", "list", "--cursor", c},
		"users with a people cursor": {"user", "list", "--cursor", c},
	} {
		t.Run(name, func(t *testing.T) {
			eo := e.refused("INVALID_VALUE", 2, args...)
			if strings.Contains(name, "cursor") || name == "other filters" || name == "other type" {
				if obj(t, eo, "details")["field"] != "cursor" {
					t.Errorf("details %v", eo["details"])
				}
			}
		})
	}
}

func TestWORKSPACE_AC_07_StrictInputAndUnknownFieldsKeptOnDisk(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)

	for _, tt := range []struct {
		args []string
		hint string
	}{
		{[]string{"person", "create", "--owner", "us_ana-acme"}, "--assignee"},
		{[]string{"person", "update", elenaID, "--delegate=us_x"}, "--agent"},
		{[]string{"organization", "create", "--name", "X", "--company", "Y"}, "--help"},
	} {
		eo := e.refused("UNKNOWN_FIELD", 2, tt.args...)
		if h, _ := eo["hint"].(string); !strings.Contains(h, tt.hint) {
			t.Errorf("%v: hint %q, want it to point to %s", tt.args, h, tt.hint)
		}
	}
	eo := e.refused("UNKNOWN_FIELD", 2, "person", "get", elenaID, "--fields", "phone")
	if d := obj(t, eo, "details"); d["field"] != "phone" || d["allowed"] == nil {
		t.Errorf("details %v", d)
	}

	// A field Kravis does not know, written by hand, survives an update.
	path := "records/people/" + elenaID + ".json"
	rec := readRecord(t, path)
	rec["nickname"] = "Ele"
	raw, _ := json.Marshal(rec)
	must(t, os.WriteFile(path, raw, 0o600))
	e.ok("person", "update", elenaID, "--name", "Elena Ruiz Vega")
	if got := readRecord(t, path); got["nickname"] != "Ele" || got["name"] != "Elena Ruiz Vega" {
		t.Errorf("after the update: %v", got)
	}
	if full := data(t, e.ok("person", "get", elenaID, "--full")); full["nickname"] != "Ele" {
		t.Errorf("--full shows the whole record: %v", full)
	}

	// Inside known nested objects too, matched by their identity: the
	// affiliation's organization, the party, the reference.
	e.ok("organization", "create", "--name", "Grupo Sierra")
	e.ok("organization", "create", "--name", "Norte Example")
	e.ok("person", "affiliation", "add", elenaID, "org_norte-example")
	e.ok("person", "affiliation", "add", elenaID, "org_grupo-sierra", "--title", "CFO")
	mark := func(path, list string, i int) {
		t.Helper()
		rec := readRecord(t, path)
		rec[list].([]any)[i].(map[string]any)["x_external_id"] = "keep-me"
		raw, _ := json.Marshal(rec)
		must(t, os.WriteFile(path, raw, 0o600))
	}
	kept := func(what, path, list, key, id string) {
		t.Helper()
		for _, it := range readRecord(t, path)[list].([]any) {
			m := it.(map[string]any)
			if m[key] == id {
				if m["x_external_id"] != "keep-me" {
					t.Errorf("%s: %s %s lost its unknown field: %v", what, list, id, m)
				}
			} else if _, ok := m["x_external_id"]; ok {
				t.Errorf("%s: the unknown field moved to %s %v", what, list, m)
			}
		}
	}
	mark(path, "affiliations", 0) // org_grupo-sierra sorts first
	e.ok("person", "update", elenaID, "--name", "Elena Example Two")
	kept("rename", path, "affiliations", "organization", "org_grupo-sierra")
	full := data(t, e.ok("person", "get", elenaID, "--full"))
	if a := full["affiliations"].([]any)[0].(map[string]any); a["x_external_id"] != "keep-me" {
		t.Errorf("--full shows the nested unknown field: %v", a)
	}
	e.ok("organization", "create", "--name", "Alba Example")
	e.ok("person", "affiliation", "add", elenaID, "org_alba-example") // sorts before, moving the others
	kept("an added affiliation", path, "affiliations", "organization", "org_grupo-sierra")
	e.ok("person", "affiliation", "add", elenaID, "org_grupo-sierra", "--title", "CEO")
	kept("a new title", path, "affiliations", "organization", "org_grupo-sierra")
	e.ok("person", "affiliation", "remove", elenaID, "org_alba-example")
	kept("a removed affiliation", path, "affiliations", "organization", "org_grupo-sierra")

	deal := "records/deals/dl_grupo-sierra.json"
	e.ok("deal", "create", "--party", "org_grupo-sierra", "--holder-role", "advisor")
	mark(deal, "parties", 0)
	e.ok("deal", "party", "add", "dl_grupo-sierra", elenaID, "--role", "client")
	kept("an added party", deal, "parties", "party", "org_grupo-sierra")
	e.ok("deal", "update", "dl_grupo-sierra", "--name", "Grupo Sierra advisory")
	kept("a renamed deal", deal, "parties", "party", "org_grupo-sierra")

	it := "records/interactions/2026-11/it_2026-11-23-kickoff.json"
	e.ok("interaction", "create", "--channel", "email", "--occurred-at", "2026-11-23T17:02:00Z", "--subject", "Kickoff",
		"--participant", elenaID, "--ref", "email:message:kickoff@example.com")
	mark(it, "references", 0)
	e.ok("interaction", "update", "it_2026-11-23-kickoff", "--subject", "Kickoff call")
	kept("a new subject", it, "references", "id", "kickoff@example.com")
}

func TestWORKSPACE_AC_08_AConcurrentChangeSurvivesAndThreeConflictsFail(t *testing.T) {
	e := newEnv(t)
	m := storage.NewMemory()
	e.Connector = m
	t.Setenv("KRAVIS_WORKSPACE", e.Workspace)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	path := domain.PersonPath(elenaID)

	// Another writer renames her between our read and our write.
	rename := func(to string) {
		var rec map[string]any
		_ = json.Unmarshal([]byte(m.Snapshot()[path]), &rec)
		rec["name"], rec["version"] = to, rec["version"].(float64)+1
		raw, _ := json.Marshal(rec)
		m.Set(path, raw)
	}
	writes := 0
	m.Hook = func(op storage.Op, p string) error {
		if op == storage.OpReplace && p == path {
			writes++
			if writes == 1 {
				rename("Elena Ruiz Vega")
			}
		}
		return nil
	}
	d := outcomeOf(t, e.ok("person", "update", elenaID, "--email", "elena@example.com", "--fields", "version"), "updated")
	if d["name"] != "Elena Ruiz Vega" || fmt.Sprint(d["emails"]) != "[e.ruiz@example.com elena@example.com]" || d["version"] != float64(3) {
		t.Errorf("the concurrent rename must survive our new email: %v", d)
	}
	if writes != 2 {
		t.Errorf("%d attempts, want 2", writes)
	}

	// It keeps changing: three attempts, then VERSION_CONFLICT.
	writes = 0
	m.Hook = func(op storage.Op, p string) error {
		if op == storage.OpReplace && p == path {
			writes++
			rename(fmt.Sprint("Elena ", writes))
		}
		return nil
	}
	eo := wantError(t, e.run("person", "update", elenaID, "--email", "other@example.com"), "VERSION_CONFLICT", 4)
	if eo["retryable"] != true || writes != 3 {
		t.Errorf("retryable %v after %d attempts", eo["retryable"], writes)
	}
	m.Hook = nil
	if _, held := m.Snapshot()[storage.Key{Kind: domain.KeyPersonEmail, Value: "other@example.com"}.Path()]; held {
		t.Error("a failed update left its key behind")
	}
}

func TestWORKSPACE_INV_13_ListsShowLiveRecordsUnlessAll(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz")
	e.ok("person", "create", "--name", "Bruno Diaz")
	outcomeOf(t, e.ok("person", "update", "pe_bruno-diaz", "--live=false"), "updated")
	e.ok("organization", "create", "--name", "Old Example")
	e.ok("organization", "update", "org_old-example", "--live=false")

	ids := func(out map[string]any) []string {
		var ids []string
		for _, r := range out["data"].([]any) {
			ids = append(ids, r.(map[string]any)["id"].(string))
		}
		return ids
	}
	if got := ids(e.ok("person", "list")); !slices.Equal(got, []string{elenaID}) {
		t.Errorf("live only: %v", got)
	}
	if got := e.ok("person", "list"); got["total"] != float64(1) {
		t.Errorf("total counts what the caller sees: %v", got["total"])
	}
	if got := ids(e.ok("person", "list", "--all")); !slices.Equal(got, []string{"pe_bruno-diaz", elenaID}) {
		t.Errorf("--all: %v", got)
	}
	if got := ids(e.ok("organization", "list")); !slices.Equal(got, []string{orgID}) {
		t.Errorf("organizations, live only: %v", got)
	}
	if got := ids(e.ok("organization", "list", "--all")); !slices.Equal(got, []string{orgID, "org_old-example"}) {
		t.Errorf("organizations, --all: %v", got)
	}
	// A record that is not live is still there to read.
	if d := data(t, e.ok("person", "get", "pe_bruno-diaz")); d["live"] != false {
		t.Errorf("get: %v", d)
	}
}

func TestDIRECTORY_INV_01_PeopleAndOrganizationsHaveNoHolder(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("organization", "create", "--name", "Other Example", "--own")
	// A user of the other holder, written by hand: users come later.
	must(t, os.WriteFile("records/users/us_bea-other.json",
		[]byte(`{"id":"us_bea-other","holder":"org_other-example","kind":"human","live":true,"version":1}`), 0o600))

	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	e.ok("organization", "create", "--name", "Grupo Sierra", "--domain", "gruposierra.example.com")
	for _, p := range []string{"records/people/" + elenaID + ".json", "records/organizations/org_grupo-sierra.json"} {
		if _, ok := readRecord(t, p)["holder"]; ok {
			t.Errorf("%s has a holder", p)
		}
	}
	// The other holder's user reads, finds and changes the same records.
	list := e.ok("--actor", "us_bea-other", "person", "list")
	if list["total"] != float64(1) || fmt.Sprint(list["filters"]) != "map[live:true]" {
		t.Errorf("shared and never limited by holder: %v", list)
	}
	outcomeOf(t, e.ok("--actor", "us_bea-other", "person", "create", "--email", elenaEmail), "existing")
	outcomeOf(t, e.ok("--actor", "us_bea-other", "organization", "update", "org_grupo-sierra", "--name", "Grupo Sierra SL"), "updated")
	if d := data(t, e.ok("organization", "get", "org_grupo-sierra", "--fields", "updated_by")); d["updated_by"] != "us_bea-other" {
		t.Errorf("get: %v", d)
	}
}

func TestDIRECTORY_INV_02_AnEmailOrADomainFindsTheExistingRecord(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail, "--email", "elena@example.com")
	// Any one of her emails finds her.
	if d := outcomeOf(t, e.ok("person", "create", "--email", "ELENA@example.com"), "existing"); d["id"] != elenaID {
		t.Errorf("%v", d)
	}
	e.ok("organization", "create", "--name", "Grupo Sierra", "--domain", "gruposierra.example.com", "--domain", "sierra.example.org")
	if d := outcomeOf(t, e.ok("organization", "create", "--name", "Grupo Sierra", "--domain", "www.sierra.example.org"), "existing"); d["id"] != "org_grupo-sierra" {
		t.Errorf("%v", d)
	}
	if n := len(e.ok("person", "list")["data"].([]any)); n != 1 {
		t.Errorf("%d people", n)
	}
}

func TestDIRECTORY_INV_03_AGenericAddressCreatesAPersonWithAWarning(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	out := e.ok("person", "create", "--email", "info@example.com")
	if d := outcomeOf(t, out, "created"); d["id"] != "pe_info-example" {
		t.Errorf("%v", d)
	}
	if !slices.Equal(warningCodes(out), []string{"GENERIC_ADDRESS"}) {
		t.Errorf("warnings %v", out["warnings"])
	}
	if out := e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail); out["warnings"] != nil {
		t.Errorf("a personal address warns of nothing: %v", out["warnings"])
	}
	if out := e.ok("person", "create", "--email", "info@example.com"); out["warnings"] != nil {
		t.Errorf("only a created person warns: %v", out)
	}
}

func TestProviderDomainsAreNeverAnOrganizationsDomain(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	eo := e.refused("INVALID_VALUE", 2, "organization", "create", "--name", "Mail Example", "--domain", "gmail.com")
	if d := obj(t, eo, "details"); d["field"] != "domain" || d["value"] != "gmail.com" {
		t.Errorf("details %v", d)
	}
	e.refused("INVALID_VALUE", 2, "organization", "update", orgID, "--domain", "www.Outlook.com")
}

func TestUpdateKeysAreReconciled(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	e.ok("person", "create", "--name", "Bruno Diaz", "--email", "bruno@example.com")

	// The second email belongs to Bruno: nothing changes, not even the first key.
	eo := e.refused("DEDUP_CONFLICT", 4, "person", "update", elenaID, "--email", "elena@example.com", "--email", "BRUNO@example.com")
	if d := obj(t, eo, "details"); d["id"] != "pe_bruno-diaz" || d["value"] != "bruno@example.com" {
		t.Errorf("details %v", d)
	}

	// Removing an email frees it for a new person; the alias follows it.
	outcomeOf(t, e.ok("person", "update", "bruno@example.com", "--remove-email", "bruno@example.com", "--email", "b.diaz@example.com"), "updated")
	if d := outcomeOf(t, e.ok("person", "create", "--name", "Bruno Lopez", "--email", "bruno@example.com"), "created"); d["id"] != "pe_bruno-lopez" {
		t.Errorf("%v", d)
	}
	if d := data(t, e.ok("person", "get", "bruno@example.com")); d["id"] != "pe_bruno-lopez" {
		t.Errorf("the alias resolves the new owner: %v", d)
	}
	if d := data(t, e.ok("person", "get", " B.Diaz@example.com")); d["id"] != "pe_bruno-diaz" {
		t.Errorf("%v", d)
	}
	if r := e.run("check"); r.Code != 0 {
		t.Errorf("check after the moves: %s", r.Stdout)
	}

	// Organizations reconcile their domains the same way.
	e.ok("organization", "create", "--name", "Grupo Sierra", "--domain", "gruposierra.example.com")
	e.ok("organization", "update", "org_grupo-sierra", "--domain", "sierra.example.org", "--remove-domain", "gruposierra.example.com")
	outcomeOf(t, e.ok("organization", "create", "--name", "Sierra Nueva", "--domain", "gruposierra.example.com"), "created")
	e.refused("DEDUP_CONFLICT", 4, "organization", "update", "org_sierra-nueva", "--domain", "sierra.example.org")
}

func TestEmailsAreAliasesOfAPerson(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	if d := data(t, e.ok("person", "get", strings.ToUpper(elenaEmail))); d["id"] != elenaID {
		t.Errorf("get: %v", d)
	}
	outcomeOf(t, e.ok("person", "update", elenaEmail, "--name", "Elena R."), "updated")
	e.refused("NOT_FOUND", 3, "person", "get", "nobody@example.com")
	e.refused("NOT_FOUND", 3, "person", "update", "nobody@example.com", "--name", "X")
	e.refused("NOT_FOUND", 3, "person", "get", "pe_nobody")
	e.refused("NOT_FOUND", 3, "person", "get", "../users/us_ana-acme")
	e.refused("NOT_FOUND", 3, "organization", "get", "org_nobody")
	outcomeOf(t, e.ok("person", "delete", elenaEmail), "deleted")
}

func TestSimilarNamesGetASuffixAndARenameKeepsTheID(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz")
	out := e.ok("person", "create", "--name", "Eléna  RUIZ")
	if d := outcomeOf(t, out, "created"); d["id"] != "pe_elena-ruiz-2" {
		t.Errorf("%v", d)
	}
	ws, _ := out["warnings"].([]any)
	if len(ws) != 1 || ws[0].(map[string]any)["code"] != "SIMILAR_EXISTS" || obj(t, ws[0], "details")["id"] != elenaID {
		t.Errorf("warnings %v", out["warnings"])
	}
	if d := data(t, e.ok("person", "create", "--name", "Elena Ruiz")); d["id"] != "pe_elena-ruiz-3" {
		t.Errorf("third: %v", d)
	}
	out = e.ok("organization", "create", "--name", orgName)
	if d := data(t, out); d["id"] != orgID+"-2" || !slices.Equal(warningCodes(out), []string{"SIMILAR_EXISTS"}) {
		t.Errorf("organization: %v", out)
	}

	d := outcomeOf(t, e.ok("person", "update", "pe_elena-ruiz-2", "--name", "Marta Gil"), "updated")
	if d["id"] != "pe_elena-ruiz-2" || d["name"] != "Marta Gil" {
		t.Errorf("rename: %v", d)
	}
	if rec := readRecord(t, "records/people/pe_elena-ruiz-2.json"); rec["name"] != "Marta Gil" {
		t.Errorf("the rename stays at its path: %v", rec)
	}
	if _, err := os.Stat("records/people/pe_marta-gil.json"); !os.IsNotExist(err) {
		t.Error("a rename moved the record")
	}
}

func TestEmptyValuesClearOptionalFields(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	d := outcomeOf(t, e.ok("person", "update", elenaID, "--name", ""), "updated")
	if _, has := d["name"]; has {
		t.Errorf("the name is cleared: %v", d)
	}
	if _, has := readRecord(t, "records/people/"+elenaID+".json")["name"]; has {
		t.Error("an empty field is omitted on disk")
	}
	// Neither a name nor an email is not a person.
	e.refused("INVALID_VALUE", 2, "person", "update", elenaID, "--remove-email", elenaEmail)
	e.refused("INVALID_VALUE", 2, "organization", "update", orgID, "--name", " ")
}

func TestInvalidInputIsRefusedBeforeWriting(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	for name, args := range map[string][]string{
		"no name or email":    {"person", "create"},
		"bad email":           {"person", "create", "--email", "not-an-email"},
		"name without ascii":  {"person", "create", "--name", "Ελένη"},
		"organization noname": {"organization", "create"},
		"organization blank":  {"organization", "create", "--name", "  "},
		"bad domain":          {"organization", "create", "--name", "X", "--domain", "https://x.example.com/"},
		"extra argument":      {"person", "get"},
	} {
		t.Run(name, func(t *testing.T) { e.refused("INVALID_VALUE", 2, args...) })
	}
	// A Greek name with an email takes its ID from the email.
	if d := data(t, e.ok("person", "create", "--name", "Ελένη", "--email", "eleni@example.com")); d["id"] != "pe_eleni-example" {
		t.Errorf("%v", d)
	}
	// Writes need an actor; reads do not.
	must(t, os.Remove(e.localConfig()))
	e.refused("MISSING_ACTOR", 2, "person", "create", "--name", "Bruno Diaz")
	e.ok("person", "list")
}

func TestAHolderCannotBeDeletedNorLoseItsOwnMark(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	eo := e.refused("REFERENCED", 4, "organization", "delete", orgID)
	if refs := obj(t, eo, "details")["referrers"]; fmt.Sprint(refs) != "["+userID+"]" {
		t.Errorf("referrers %v", refs)
	}
	e.refused("REFERENCED", 4, "organization", "update", orgID, "--own=false")
	// An own organization that is no holder can lose the mark, and go.
	e.ok("organization", "create", "--name", "Spare Example", "--own")
	outcomeOf(t, e.ok("organization", "update", "org_spare-example", "--own=false"), "updated")
	outcomeOf(t, e.ok("organization", "delete", "org_spare-example"), "deleted")
}

func TestFieldsAndFullWidenTheView(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	d := data(t, e.ok("person", "create", "--name", "Elena Ruiz"))
	if fmt.Sprint(keysOf(d)) != "[emails id live name]" {
		t.Errorf("compact view: %v", d)
	}
	d = data(t, e.ok("person", "get", elenaID, "--fields", "version,created_by"))
	if d["version"] != float64(1) || d["created_by"] != userID || d["updated_at"] != nil {
		t.Errorf("--fields: %v", d)
	}
	d = data(t, e.ok("person", "get", elenaID, "--full"))
	for _, k := range []string{"id", "name", "live", "version", "created_at", "created_by", "updated_at", "updated_by"} {
		if _, ok := d[k]; !ok {
			t.Errorf("--full lacks %s: %v", k, d)
		}
	}
	list := e.ok("person", "list", "--fields", "version")
	if r := list["data"].([]any)[0].(map[string]any); r["version"] != float64(1) {
		t.Errorf("list --fields: %v", r)
	}
	if d := data(t, e.ok("organization", "get", orgID)); fmt.Sprint(keysOf(d)) != "[domains id live name own]" {
		t.Errorf("organization view: %v", d)
	}
}

func keysOf(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }

func TestCheckReportsBrokenPeopleAndOrganizations(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	e.ok("person", "create", "--name", "Bruno Diaz", "--email", "bruno@example.com")
	e.ok("organization", "create", "--name", "Grupo Sierra", "--domain", "gruposierra.example.com")
	if r := e.run("check"); r.Code != 0 {
		t.Fatalf("clean: %s", r.Stdout)
	}

	// A value without its key: an email added by hand.
	bruno := readRecord(t, "records/people/pe_bruno-diaz.json")
	bruno["emails"] = []string{"bruno@example.com", "hand@example.com"}
	raw, _ := json.Marshal(bruno)
	must(t, os.WriteFile("records/people/pe_bruno-diaz.json", raw, 0o600))
	// A key without its value: a domain removed by hand.
	org := readRecord(t, "records/organizations/org_grupo-sierra.json")
	delete(org, "domains")
	raw, _ = json.Marshal(org)
	must(t, os.WriteFile("records/organizations/org_grupo-sierra.json", raw, 0o600))
	// Records that break their rules.
	must(t, os.WriteFile("records/people/pe_nobody.json", withMeta(`{"id":"pe_nobody","live":true}`), 0o600))
	must(t, os.WriteFile("records/people/pe_moved.json", withMeta(`{"id":"pe_other","name":"X","live":true}`), 0o600))
	must(t, os.WriteFile("records/organizations/org_mail.json", withMeta(`{"id":"org_mail","name":"Mail","domains":["gmail.com"],"live":true}`), 0o600))
	must(t, os.WriteFile("records/people/pe_typed.json", withMeta(`{"id":"pe_typed","name":123,"live":true}`), 0o600))
	must(t, os.WriteFile("records/people/pe_nolive.json", withMeta(`{"id":"pe_nolive","name":"No Live"}`), 0o600))
	must(t, os.WriteFile("records/people/pe_nulllive.json", withMeta(`{"id":"pe_nulllive","name":"Null Live","live":null}`), 0o600))
	// A key whose record is gone.
	must(t, os.Remove("records/people/"+elenaID+".json"))

	before := e.Files()
	r := e.run("check")
	if r.Code != 6 {
		t.Fatalf("code %d: %s %s", r.Code, r.Stdout, r.Stderr)
	}
	var got []string
	for _, f := range r.JSON(t)["data"].([]any) {
		m := f.(map[string]any)
		rec := m["record"].(string)
		if strings.HasPrefix(rec, "keys/") {
			rec = "keys/" + strings.Split(rec, "/")[1]
		}
		got = append(got, m["code"].(string)+" "+rec)
	}
	slices.Sort(got)
	want := []string{
		"INVALID_RECORD records/organizations/org_mail.json",
		"INVALID_RECORD records/people/pe_moved.json",
		"INVALID_RECORD records/people/pe_nobody.json",
		"INVALID_RECORD records/people/pe_nolive.json",
		"INVALID_RECORD records/people/pe_nulllive.json",
		"INVALID_RECORD records/people/pe_typed.json",
		"KEY_WITHOUT_VALUE keys/organization-domain",
		"KEY_WITHOUT_VALUE keys/person-email",
		"VALUE_WITHOUT_KEY records/organizations/org_mail.json",
		"VALUE_WITHOUT_KEY records/people/pe_bruno-diaz.json",
	}
	if !slices.Equal(got, want) {
		t.Errorf("findings\ngot  %v\nwant %v", got, want)
	}
	if !maps.Equal(before, e.Files()) {
		t.Error("check wrote")
	}
}

func TestListsOnATerminalAndSkippedFiles(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	e.ok("person", "create", "--name", "Elena Ruiz")
	must(t, os.WriteFile("records/people/pe_broken.json", []byte("{nope"), 0o600))
	must(t, os.WriteFile("records/people/pe_note.md", []byte("text"), 0o600))
	if out := e.ok("person", "list"); out["total"] != float64(1) {
		t.Errorf("a broken file is left to check: %v", out)
	}
	// A row without a name does not hide the name column.
	e.ok("person", "create", "--email", "a@example.com")
	e.TTY = true
	r := e.run("person", "list", "--limit", "1")
	if r.Code != 0 || !strings.Contains(r.Stdout, "total") ||
		!strings.Contains(r.Stdout, "--cursor ") || strings.HasPrefix(r.Stdout, "{") {
		t.Errorf("table: %q", r.Stdout)
	}
	if r := e.run("person", "list"); !strings.Contains(r.Stdout, "NAME") || !strings.Contains(r.Stdout, "Elena Ruiz") || !strings.Contains(r.Stdout, "last page") {
		t.Errorf("last page: %q", r.Stdout)
	}
	e.TTY = false
	// The largest limit is a valid limit.
	c := e.ok("person", "list", "--limit", "1")["next_cursor"].(string)
	if out := e.ok("person", "list", "--limit", "9223372036854775807", "--cursor", c); len(out["data"].([]any)) != 1 || out["next_cursor"] != nil {
		t.Errorf("huge limit: %v", out)
	}
}

func TestConcurrentCreatesTakeSuffixesOrFindTheSameEmail(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	runAll := func(args ...string) []map[string]any {
		outs := make([]map[string]any, 6)
		var wg sync.WaitGroup
		for i := range outs {
			wg.Go(func() {
				r := e.run(args...)
				if r.Code != 0 {
					t.Errorf("%v: %d %s", args, r.Code, r.Stderr)
					return
				}
				outs[i] = r.JSON(t)
			})
		}
		wg.Wait()
		return outs
	}
	ids := map[string]bool{}
	for _, out := range runAll("person", "create", "--name", "Elena Ruiz") {
		if out != nil {
			ids[data(t, out)["id"].(string)] = true
		}
	}
	if len(ids) != 6 || !ids[elenaID] || !ids[elenaID+"-6"] {
		t.Errorf("same name: %v", ids)
	}
	created := 0
	for _, out := range runAll("person", "create", "--email", "bea@example.com") {
		if out != nil && out["outcome"] == "created" {
			created++
		}
	}
	if created != 1 {
		t.Errorf("same email: %d created", created)
	}
	if r := e.run("check"); r.Code != 0 {
		t.Errorf("check: %s", r.Stdout)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
