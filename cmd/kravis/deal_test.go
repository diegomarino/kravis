package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

// Deal fixtures: Grupo Sierra is the first party of the usual deal.
const (
	sierraDeal = "dl_grupo-sierra"
	sierraFile = "records/deals/dl_grupo-sierra.json"
)

// mkDeal is the args of a deal create with Grupo Sierra as its party, as
// holder A's human; extra flags follow.
func mkDeal(extra ...string) []string {
	return slices.Concat([]string{"deal", "create", "--party", sierraID, "--holder-role", "advisor"}, extra)
}

// setCatalog replaces one catalog of config.json.
func (e *env) setCatalog(name string, value any) {
	e.t.Helper()
	var cfg map[string]any
	raw, err := os.ReadFile("config.json")
	must(e.t, err)
	must(e.t, json.Unmarshal(raw, &cfg))
	if value == nil {
		delete(cfg["catalogs"].(map[string]any), name)
	} else {
		cfg["catalogs"].(map[string]any)[name] = value
	}
	writeRecord(e.t, "config.json", cfg)
}

func partiesOf(t *testing.T, id string) string {
	t.Helper()
	return fmt.Sprint(readRecord(t, "records/deals/"+id+".json")["parties"])
}

func TestDEALS_INV_01_ADealIsInOnePipelineAndOneOfItsStages(t *testing.T) {
	e := world(t)
	// init writes the built-in pipeline.
	cfg := readRecord(t, "config.json")
	stages := fmt.Sprint(obj(t, obj(t, cfg["catalogs"], "pipelines"), "default")["stages"])
	if stages != "[map[category:ongoing name:prospect] map[category:ongoing name:engaged] map[category:ongoing name:closing] map[category:won name:won] map[category:lost name:lost]]" {
		t.Errorf("stages %s", stages)
	}
	d := outcomeOf(t, e.ok(mkDeal()...), "created")
	if d["id"] != sierraDeal || d["stage"] != "prospect" || d["category"] != "ongoing" || d["live"] != true {
		t.Errorf("created %v", d)
	}
	if rec := readRecord(t, sierraFile); rec["pipeline"] != "default" {
		t.Errorf("pipeline %v", rec["pipeline"])
	}
	// Another first stage can be given, but not one outside the pipeline, nor a lost one.
	if d := data(t, e.ok(mkDeal("--stage", "closing")...)); d["stage"] != "closing" || d["id"] != sierraDeal+"-2" {
		t.Errorf("closing %v", d)
	}
	eo := e.refused("INVALID_VALUE", 2, mkDeal("--stage", "signed")...)
	if det := details(t, eo); det["field"] != "stage" || fmt.Sprint(det["allowed"]) != "[prospect engaged closing won lost]" {
		t.Errorf("details %v", det)
	}
	eo = e.refused("INVALID_VALUE", 2, mkDeal("--stage", "lost")...)
	if !strings.Contains(fmt.Sprint(eo["hint"]), "deal stage") {
		t.Errorf("hint %v", eo["hint"])
	}
	// A workspace from before deals has no pipeline in config.json: the built-in one applies.
	e.setCatalog("pipelines", nil)
	e.setCatalog("party_roles", nil)
	if d := data(t, e.ok(mkDeal()...)); d["stage"] != "prospect" {
		t.Errorf("without pipelines %v", d)
	}
	if got := e.findings(); len(got) != 0 {
		t.Errorf("findings %v", got)
	}
	// A pipeline with a stage of an unknown category makes config.json invalid.
	e.setCatalog("pipelines", map[string]any{"default": map[string]any{"stages": []any{map[string]any{"name": "x", "category": "paused"}}}})
	if got := e.findings(); len(got["WORKSPACE_FILE_INVALID"]) != 1 {
		t.Errorf("findings %v", got)
	}
}

func TestDEALS_INV_02_AStageChangesOnlyThroughItsTransitionToAnyStage(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	for _, c := range []struct{ stage, category string }{{"won", "won"}, {"prospect", "ongoing"}, {"closing", "ongoing"}} {
		d := outcomeOf(t, e.ok("deal", "stage", sierraDeal, c.stage), "updated")
		if d["stage"] != c.stage || d["category"] != c.category {
			t.Errorf("%s: %v", c.stage, d)
		}
	}
	// The same stage again is unchanged and keeps the version.
	before := readRecord(t, sierraFile)["version"]
	outcomeOf(t, e.ok("deal", "stage", sierraDeal, "closing"), "unchanged")
	if v := readRecord(t, sierraFile)["version"]; v != before {
		t.Errorf("version %v, want %v", v, before)
	}
	eo := e.refused("INVALID_VALUE", 2, "deal", "stage", sierraDeal, "signed")
	if fmt.Sprint(details(t, eo)["allowed"]) != "[prospect engaged closing won lost]" {
		t.Errorf("details %v", eo["details"])
	}
	// update has no stage, and no live flag.
	e.refused("UNKNOWN_FIELD", 2, "deal", "update", sierraDeal, "--stage", "won")
	e.refused("UNKNOWN_FIELD", 2, "deal", "update", sierraDeal, "--live=false")
}

func TestDEALS_AC_01_UpdateCannotChangeTheStageAndLostWithoutAReasonListsTheReasons(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	eo := e.refused("UNKNOWN_FIELD", 2, "deal", "update", sierraDeal, "--stage", "lost")
	if details(t, eo)["field"] != "stage" || !strings.Contains(fmt.Sprint(eo["hint"]), "kravis deal stage") {
		t.Errorf("update --stage: %v", eo)
	}
	eo = e.refused("INVALID_VALUE", 2, "deal", "stage", sierraDeal, "lost")
	if det := details(t, eo); det["field"] != "reason" || fmt.Sprint(det["allowed"]) != "[price timing competitor no-decision ghosting fit other]" {
		t.Errorf("details %v", det)
	}
	if rec := readRecord(t, sierraFile); rec["stage"] != "prospect" {
		t.Errorf("stage %v", rec["stage"])
	}
}

func TestDEALS_INV_03_ALostStageNeedsALostReasonAndLeavingItClearsIt(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	eo := e.refused("INVALID_VALUE", 2, "deal", "stage", sierraDeal, "lost", "--reason", "bad-luck")
	if det := details(t, eo); det["field"] != "reason" || len(det["allowed"].([]any)) != 7 {
		t.Errorf("details %v", det)
	}
	// A reason is only for a lost stage.
	eo = e.refused("INVALID_VALUE", 2, "deal", "stage", sierraDeal, "engaged", "--reason", "price")
	if details(t, eo)["field"] != "reason" {
		t.Errorf("details %v", eo["details"])
	}
	d := outcomeOf(t, e.ok("deal", "stage", sierraDeal, "lost", "--reason", "price"), "updated")
	if d["lost_reason"] != "price" || d["category"] != "lost" || d["live"] != false {
		t.Errorf("lost %v", d)
	}
	outcomeOf(t, e.ok("deal", "stage", sierraDeal, "lost", "--reason", "price"), "unchanged")
	if d := outcomeOf(t, e.ok("deal", "stage", sierraDeal, "lost", "--reason", "timing"), "updated"); d["lost_reason"] != "timing" {
		t.Errorf("replaced %v", d)
	}
	for _, stage := range []string{"engaged", "lost", "won"} {
		args := []string{"deal", "stage", sierraDeal, stage}
		if stage == "lost" {
			args = append(args, "--reason", "fit")
		}
		e.ok(args...)
		rec := readRecord(t, sierraFile)
		if _, has := rec["lost_reason"]; has != (stage == "lost") {
			t.Errorf("%s: lost_reason %v", stage, rec["lost_reason"])
		}
	}
	// A record that breaks the rule by hand is reported.
	rec := readRecord(t, sierraFile) // won
	rec["lost_reason"] = "price"
	writeRecord(t, sierraFile, rec)
	if got := e.findings(); !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "not in the lost category") {
		t.Errorf("findings %v", got)
	}
	rec["stage"], rec["category"], rec["live"] = "lost", "lost", false
	delete(rec, "lost_reason")
	writeRecord(t, sierraFile, rec)
	if got := e.findings(); !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "needs a lost reason") {
		t.Errorf("findings %v", got)
	}
}

func TestDEALS_INV_04_ADealRecordsItsHoldersPartyRoleFromTheCatalog(t *testing.T) {
	e := world(t)
	eo := e.refused("INVALID_VALUE", 2, "deal", "create", "--party", sierraID)
	if det := details(t, eo); det["field"] != "holder_role" || len(det["allowed"].([]any)) != 7 {
		t.Errorf("details %v", det)
	}
	eo = e.refused("INVALID_VALUE", 2, mkDeal("--holder-role", "boss")...)
	if det := details(t, eo); det["field"] != "holder_role" || fmt.Sprint(det["allowed"]) != "[client counterparty advisor lender investor buyer seller]" {
		t.Errorf("details %v", det)
	}
	if d := data(t, e.ok(mkDeal()...)); d["holder_role"] != "advisor" {
		t.Errorf("created %v", d)
	}
	if d := outcomeOf(t, e.ok("deal", "update", sierraDeal, "--holder-role", "seller"), "updated"); d["holder_role"] != "seller" {
		t.Errorf("updated %v", d)
	}
	e.refused("INVALID_VALUE", 2, "deal", "update", sierraDeal, "--holder-role", "boss")
	rec := readRecord(t, sierraFile)
	delete(rec, "holder_role")
	writeRecord(t, sierraFile, rec)
	if got := e.findings(); !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "holder role") {
		t.Errorf("findings %v", got)
	}
}

func TestDEALS_INV_05_APartyIsOncePerDealWithItsRolesAndNeverTheHolder(t *testing.T) {
	e := world(t)
	d := outcomeOf(t, e.ok(mkDeal("--role", "seller")...), "created")
	if fmt.Sprint(d["parties"]) != "[map[party:org_grupo-sierra roles:[seller]]]" {
		t.Errorf("created %v", d)
	}
	// A person by email, with two roles; the parties stay sorted by ID.
	d = outcomeOf(t, e.ok("deal", "party", "add", sierraDeal, elenaEmail, "--role", "client", "--role", "advisor"), "updated")
	want := "[map[party:org_grupo-sierra roles:[seller]] map[party:pe_elena-ruiz roles:[advisor client]]]"
	if fmt.Sprint(d["parties"]) != want || partiesOf(t, sierraDeal) != want {
		t.Errorf("added %v", d["parties"])
	}
	// The same person again adds only the missing roles, and nothing when all are there.
	outcomeOf(t, e.ok("deal", "party", "add", sierraDeal, elenaID, "--role", "client"), "unchanged")
	outcomeOf(t, e.ok("deal", "party", "add", sierraDeal, elenaID), "unchanged")
	outcomeOf(t, e.ok("deal", "party", "add", sierraDeal, elenaID, "--role", "buyer", "--role", "client"), "updated")
	if got := partiesOf(t, sierraDeal); !strings.Contains(got, "pe_elena-ruiz roles:[advisor buyer client]") {
		t.Errorf("merged %v", got)
	}
	// An organization without roles.
	e.ok("deal", "party", "add", sierraDeal, "org_norte-example")
	// The holder is never a party, on create or after.
	eo := e.refused("INVALID_VALUE", 2, "deal", "party", "add", sierraDeal, orgID)
	if details(t, eo)["field"] != "party" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("INVALID_VALUE", 2, "deal", "create", "--party", orgID, "--holder-role", "advisor")
	// Another own organization can be a party.
	e.ok("deal", "party", "add", sierraDeal, otherOrg)
	// A party exists and is a person or an organization; roles come from the catalog.
	e.refused("NOT_FOUND", 3, "deal", "party", "add", sierraDeal, "pe_nobody")
	e.refused("NOT_FOUND", 3, "deal", "party", "add", sierraDeal, "nobody@example.com")
	e.refused("NOT_FOUND", 3, "deal", "create", "--party", "org_nowhere", "--holder-role", "advisor")
	e.refused("INVALID_VALUE", 2, "deal", "party", "add", sierraDeal, userID)
	eo = e.refused("INVALID_VALUE", 2, "deal", "party", "add", sierraDeal, elenaID, "--role", "friend")
	if det := details(t, eo); det["field"] != "role" || len(det["allowed"].([]any)) != 7 {
		t.Errorf("details %v", det)
	}
	e.refused("INVALID_VALUE", 2, mkDeal("--role", "friend")...)

	// Removing one role keeps the party, even with none left; removing the party drops it.
	e.ok("deal", "party", "remove", sierraDeal, sierraID, "--role", "seller")
	if got := partiesOf(t, sierraDeal); !strings.Contains(got, "map[party:org_grupo-sierra]") {
		t.Errorf("role removed %v", got)
	}
	e.refused("NOT_FOUND", 3, "deal", "party", "remove", sierraDeal, sierraID, "--role", "seller")
	e.ok("deal", "party", "remove", sierraDeal, sierraID)
	if got := partiesOf(t, sierraDeal); strings.Contains(got, sierraID) {
		t.Errorf("party removed %v", got)
	}
	eo = e.refused("NOT_FOUND", 3, "deal", "party", "remove", sierraDeal, sierraID)
	if details(t, eo)["field"] != "party" {
		t.Errorf("details %v", eo["details"])
	}
	// The ID never changes, and a second deal with the same party gets -2 without a warning.
	if d := data(t, e.ok("deal", "get", sierraDeal)); d["id"] != sierraDeal {
		t.Errorf("get %v", d)
	}
	out := e.ok(mkDeal()...)
	if idOf(t, out) != sierraDeal+"-2" || len(warningCodes(out)) != 0 {
		t.Errorf("second deal %v", out)
	}

	// check reports a deal edited by hand into breaking the rule.
	rec := readRecord(t, sierraFile)
	rec["parties"] = []any{map[string]any{"party": orgID}, map[string]any{"party": elenaID}, map[string]any{"party": elenaID}}
	writeRecord(t, sierraFile, rec)
	got := strings.Join(e.findings()["INVALID_RECORD"], "\n")
	if !strings.Contains(got, "is one of the deal's parties") || !strings.Contains(got, "pe_elena-ruiz is repeated") {
		t.Errorf("findings %v", got)
	}
}

func TestWORKSPACE_INV_02_AnActorWritesOnlyItsOwnHoldersDeals(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	for _, flags := range [][]string{nil, {"--all-holders"}} {
		for _, cmd := range [][]string{
			{"deal", "update", sierraDeal, "--name", "Taken over"},
			{"deal", "stage", sierraDeal, "won"},
			{"deal", "delete", sierraDeal},
		} {
			e.refused("NOT_FOUND", 3, as(otherAgent, slices.Concat(cmd, flags)...)...)
		}
	}
	e.refused("NOT_FOUND", 3, as(otherAgent, "deal", "party", "add", sierraDeal, elenaID)...)
	e.refused("NOT_FOUND", 3, as(otherAgent, "deal", "party", "remove", sierraDeal, sierraID)...)
	if rec := readRecord(t, sierraFile); rec["version"] != float64(1) {
		t.Errorf("changed: %v", rec)
	}
	// Reads are limited too, unless they ask for all holders.
	e.refused("NOT_FOUND", 3, as(otherAgent, "deal", "get", sierraDeal)...)
	e.ok(as(otherAgent, "deal", "get", sierraDeal, "--all-holders")...)
	if l := e.ok(as(otherAgent, "deal", "list")...); l["total"] != float64(0) || fmt.Sprint(l["filters"]) != "map[holder:"+otherOrg+" live:true]" {
		t.Errorf("list %v", l)
	}
	// A deal takes its holder from its actor, and that holder can be a party of another holder's deal.
	d := outcomeOf(t, e.ok(as(otherAgent, "deal", "create", "--party", orgID, "--holder-role", "buyer")...), "created")
	if d["holder"] != otherOrg || d["id"] != "dl_acme-example" {
		t.Errorf("created %v", d)
	}
}

func TestWORKSPACE_AC_04_DealWritesReturnTheirOutcome(t *testing.T) {
	e := world(t)
	out := e.ok(mkDeal("--name", "Sierra sale")...)
	if d := outcomeOf(t, out, "created"); d["name"] != "Sierra sale" || d["holder"] != orgID {
		t.Errorf("created %v", d)
	}
	// No natural key: the same create makes another deal.
	outcomeOf(t, e.ok(mkDeal("--name", "Sierra sale")...), "created")
	outcomeOf(t, e.ok("deal", "update", sierraDeal, "--name", "Sierra exit"), "updated")
	outcomeOf(t, e.ok("deal", "update", sierraDeal, "--name", "Sierra exit"), "unchanged")
	if v := readRecord(t, sierraFile)["version"]; v != float64(2) {
		t.Errorf("version %v", v)
	}
	// The description lives in the sidecar; "" clears the name, which the view then omits.
	d := outcomeOf(t, e.runOK("Notes on the deal\n", "deal", "update", sierraDeal, "--description-file", "-", "--name", ""), "updated")
	if _, has := d["name"]; has || d["description"] != "Notes on the deal\n" {
		t.Errorf("update %v", d)
	}
	if d := data(t, e.ok("deal", "get", sierraDeal, "--full")); d["description"] != "Notes on the deal\n" || d["pipeline"] != "default" {
		t.Errorf("full %v", d)
	}
	if d := data(t, e.ok("deal", "get", sierraDeal)); strings.Join(keysOf(d), ",") != "category,holder,holder_role,id,live,stage" {
		t.Errorf("compact %v", keysOf(d))
	}
	d = outcomeOf(t, e.ok("deal", "delete", sierraDeal, "--full"), "deleted")
	if d["description"] != "Notes on the deal\n" || exists(sierraFile) || exists("records/deals/dl_grupo-sierra.md") {
		t.Errorf("deleted %v", d)
	}
	e.refused("NOT_FOUND", 3, "deal", "get", sierraDeal)
	e.refused("UNKNOWN_FIELD", 2, "deal", "get", sierraDeal+"-2", "--fields", "value")
}

// runOK runs a command with stdin that must succeed.
func (e *env) runOK(stdin string, args ...string) map[string]any {
	e.t.Helper()
	r := e.runWithStdin(stdin, args...)
	if r.Code != 0 {
		e.t.Fatalf("%v: code %d, stderr %q", args, r.Code, r.Stderr)
	}
	return r.JSON(e.t)
}

func TestWORKSPACE_AC_05_DealsListByNameWithFiltersAndACursor(t *testing.T) {
	e := world(t)
	e.ok(mkDeal("--name", "Zeta")...)                                                        // dl_grupo-sierra
	e.ok(mkDeal("--name", "Álamo")...)                                                       // -2
	e.ok(mkDeal()...)                                                                        // -3, no name: by ID
	e.ok("deal", "create", "--party", elenaID, "--holder-role", "advisor", "--name", "Beta") // dl_elena-ruiz
	e.ok("deal", "stage", sierraDeal+"-2", "won")
	e.ok("deal", "stage", sierraDeal+"-3", "lost", "--reason", "price")
	e.ok(as(otherAgent, "deal", "create", "--party", sierraID, "--holder-role", "buyer")...)

	l := e.ok("deal", "list")
	if got := ids(t, l); fmt.Sprint(got) != "[dl_elena-ruiz dl_grupo-sierra]" || l["total"] != float64(2) {
		t.Errorf("live %v %v", got, l["total"])
	}
	all := ids(t, e.ok("deal", "list", "--all"))
	if fmt.Sprint(all) != "[dl_grupo-sierra-2 dl_elena-ruiz dl_grupo-sierra-3 dl_grupo-sierra]" {
		t.Errorf("all %v", all)
	}
	// Two pages of two continue with the cursor.
	p1 := e.ok("deal", "list", "--all", "--limit", "2")
	p2 := e.ok("deal", "list", "--all", "--limit", "2", "--cursor", p1["next_cursor"].(string))
	if got := append(ids(t, p1), ids(t, p2)...); !slices.Equal(got, all) || p2["next_cursor"] != nil || p1["total"] != float64(4) {
		t.Errorf("pages %v", got)
	}
	e.refused("INVALID_VALUE", 2, "deal", "list", "--limit", "2", "--cursor", p1["next_cursor"].(string))
	// Filters: a stage or a category includes deals that are not live.
	for args, want := range map[string]string{
		"--stage won":                    "[dl_grupo-sierra-2]",
		"--category lost":                "[dl_grupo-sierra-3]",
		"--party " + elenaEmail:          "[dl_elena-ruiz]",
		"--party " + sierraID:            "[dl_grupo-sierra]",
		"--party " + sierraID + " --all": "[dl_grupo-sierra-2 dl_grupo-sierra-3 dl_grupo-sierra]",
	} {
		if got := ids(t, e.ok(append([]string{"deal", "list"}, strings.Fields(args)...)...)); fmt.Sprint(got) != want {
			t.Errorf("%s: %v, want %s", args, got, want)
		}
	}
	e.refused("INVALID_VALUE", 2, "deal", "list", "--category", "open")
	e.refused("INVALID_VALUE", 2, "deal", "list", "--stage", "signed")
	if got := ids(t, e.ok("deal", "list", "--all-holders")); len(got) != 3 {
		t.Errorf("all holders %v", got)
	}
}

func TestWORKSPACE_AC_10_DeletingAPartyOfADealIsRefusedAndListsTheDeal(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	e.ok("deal", "party", "add", sierraDeal, "pe_marco-silva")
	e.ok("person", "affiliation", "remove", "pe_marco-silva", sierraID)
	e.ok("person", "affiliation", "remove", "pe_marco-silva", "org_norte-example")
	eo := e.refused("REFERENCED", 4, "person", "delete", "pe_marco-silva")
	if fmt.Sprint(details(t, eo)["referrers"]) != "["+sierraDeal+"]" {
		t.Errorf("details %v", eo["details"])
	}
	// A holder with deals keeps its own mark, like one with tasks.
	e.ok(as(otherAgent, "deal", "create", "--party", sierraID, "--holder-role", "buyer")...)
	eo = e.refused("REFERENCED", 4, "organization", "update", otherOrg, "--own=false")
	if !strings.Contains(fmt.Sprint(details(t, eo)["referrers"]), sierraDeal) {
		t.Errorf("details %v", eo["details"])
	}
	e.ok("deal", "party", "remove", sierraDeal, "pe_marco-silva")
	outcomeOf(t, e.ok("person", "delete", "pe_marco-silva"), "deleted")
}

func TestWORKSPACE_AC_13_ADealValueOutsideItsCatalogIsRefusedWithTheAllowedList(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	for _, c := range []struct {
		field string
		args  []string
	}{
		{"stage", []string{"deal", "stage", sierraDeal, "signed"}},
		{"reason", []string{"deal", "stage", sierraDeal, "lost", "--reason", "bad-luck"}},
		{"role", []string{"deal", "party", "add", sierraDeal, elenaID, "--role", "friend"}},
		{"holder_role", []string{"deal", "update", sierraDeal, "--holder-role", "boss"}},
	} {
		det := details(t, e.refused("INVALID_VALUE", 2, c.args...))
		if det["field"] != c.field || len(det["allowed"].([]any)) == 0 {
			t.Errorf("%s: %v", c.field, det)
		}
	}
}

func TestWORKSPACE_INV_05_ADealValueRemovedFromTheCatalogStaysAndMustBeReplaced(t *testing.T) {
	e := world(t)
	e.ok(mkDeal("--role", "seller")...)
	e.ok("deal", "stage", sierraDeal, "lost", "--reason", "ghosting")
	e.setCatalog("lost_reasons", []string{"price", "timing"})
	got := e.findings()
	if r := strings.Join(got["VALUE_NOT_IN_CATALOG"], "\n"); !strings.Contains(r, `"ghosting"`) || !strings.Contains(r, orgID) {
		t.Errorf("findings %v", got)
	}
	// It stays: the deal can be read, and the record keeps it.
	if d := data(t, e.ok("deal", "get", sierraDeal)); d["lost_reason"] != "ghosting" {
		t.Errorf("get %v", d)
	}
	eo := e.refused("INVALID_VALUE", 2, "deal", "update", sierraDeal, "--name", "Sierra")
	if det := details(t, eo); det["field"] != "reason" || fmt.Sprint(det["allowed"]) != "[price timing]" || !strings.Contains(fmt.Sprint(eo["hint"]), "deal stage") {
		t.Errorf("update: %v", eo)
	}
	e.refused("INVALID_VALUE", 2, "deal", "stage", sierraDeal, "lost", "--reason", "ghosting")
	outcomeOf(t, e.ok("deal", "stage", sierraDeal, "lost", "--reason", "timing"), "updated")
	outcomeOf(t, e.ok("deal", "update", sierraDeal, "--name", "Sierra"), "updated")

	// A party role and the holder role: update refuses until they are replaced.
	e.setCatalog("party_roles", []string{"client", "buyer"})
	got = e.findings()
	if r := strings.Join(got["VALUE_NOT_IN_CATALOG"], "\n"); !strings.Contains(r, `"seller"`) || !strings.Contains(r, `"advisor"`) {
		t.Errorf("findings %v", got)
	}
	eo = e.refused("INVALID_VALUE", 2, "deal", "update", sierraDeal, "--name", "Sierra 2")
	if details(t, eo)["field"] != "holder_role" {
		t.Errorf("holder role: %v", eo)
	}
	eo = e.refused("INVALID_VALUE", 2, "deal", "update", sierraDeal, "--holder-role", "buyer")
	if details(t, eo)["field"] != "role" || !strings.Contains(fmt.Sprint(eo["hint"]), "--role seller") {
		t.Errorf("party role: %v", eo)
	}
	e.ok("deal", "party", "remove", sierraDeal, sierraID, "--role", "seller")
	outcomeOf(t, e.ok("deal", "update", sierraDeal, "--holder-role", "buyer"), "updated")

	// A stage that leaves the pipeline: only deal stage moves the deal on.
	e.ok("deal", "stage", sierraDeal, "engaged")
	e.setCatalog("pipelines", map[string]any{"default": map[string]any{"stages": []any{
		map[string]any{"name": "prospect", "category": "ongoing"}, map[string]any{"name": "won", "category": "won"},
		map[string]any{"name": "lost", "category": "lost"}}}})
	if r := strings.Join(e.findings()["VALUE_NOT_IN_CATALOG"], "\n"); !strings.Contains(r, `"engaged"`) {
		t.Errorf("stage finding %v", r)
	}
	eo = e.refused("INVALID_VALUE", 2, "deal", "update", sierraDeal, "--name", "Sierra 3")
	if det := details(t, eo); det["field"] != "stage" || fmt.Sprint(det["allowed"]) != "[prospect won lost]" {
		t.Errorf("stage: %v", eo)
	}
	e.ok("deal", "stage", sierraDeal, "prospect")
	outcomeOf(t, e.ok("deal", "update", sierraDeal, "--name", "Sierra 3"), "updated")
	if got := e.findings(); len(got) != 0 {
		t.Errorf("clean again: %v", got)
	}
}

func TestWORKSPACE_INV_06_ADealStoresItsCategoryAndLiveFromItsStage(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	for _, c := range []struct {
		stage, category string
		live            bool
	}{{"engaged", "ongoing", true}, {"won", "won", false}, {"closing", "ongoing", true}} {
		e.ok("deal", "stage", sierraDeal, c.stage)
		rec := readRecord(t, sierraFile)
		if rec["category"] != c.category || rec["live"] != c.live {
			t.Errorf("%s: %v %v", c.stage, rec["category"], rec["live"])
		}
	}
	rec := readRecord(t, sierraFile) // closing
	rec["category"] = "won"
	rec["live"] = false
	writeRecord(t, sierraFile, rec)
	if got := e.findings(); !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "disagrees with the catalog") {
		t.Errorf("category: %v", got)
	}
	rec["category"] = "ongoing"
	writeRecord(t, sierraFile, rec)
	if got := e.findings(); !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "live is false") {
		t.Errorf("live: %v", got)
	}
}

func TestDealCheckReportsBrokenReferences(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	rec := readRecord(t, sierraFile)
	rec["holder"] = "org_gone"
	rec["parties"] = []any{map[string]any{"party": "pe_gone"}, map[string]any{"party": "org_norte-example"}}
	writeRecord(t, sierraFile, rec)
	got := strings.Join(e.findings()["BROKEN_REFERENCE"], "\n")
	if !strings.Contains(got, "holder org_gone") || !strings.Contains(got, "party pe_gone") || strings.Contains(got, "org_norte-example") {
		t.Errorf("findings %v", got)
	}
	rec["holder"] = sierraID // not an own organization
	writeRecord(t, sierraFile, rec)
	if got := strings.Join(e.findings()["INVALID_RECORD"], "\n"); !strings.Contains(got, "not an own organization") {
		t.Errorf("findings %v", got)
	}
}

func TestDealPartyWritesFollowTheCatalogAndShowTheDescription(t *testing.T) {
	e := world(t)
	e.runOK("Notes\n", append(mkDeal(), "--description-file", "-")...)
	// The catalog moves prospect to won: the next party write follows it.
	e.setCatalog("pipelines", map[string]any{"default": map[string]any{"stages": []any{
		map[string]any{"name": "prospect", "category": "won"}, map[string]any{"name": "lost", "category": "lost"}}}})
	d := data(t, e.ok("deal", "party", "add", sierraDeal, elenaID, "--full"))
	if d["category"] != "won" || d["live"] != false || d["description"] != "Notes\n" {
		t.Errorf("add %v", d)
	}
	if d := data(t, e.ok("deal", "party", "remove", sierraDeal, elenaID, "--fields", "description")); d["description"] != "Notes\n" {
		t.Errorf("remove %v", d)
	}
	if got := e.findings(); len(got) != 0 {
		t.Errorf("findings %v", got)
	}
}

func TestTerminalListsShowTheirFiltersAlsoWhenEmpty(t *testing.T) {
	e := world(t)
	e.ok(mkDeal()...)
	e.ok("deal", "stage", sierraDeal, "won")
	e.TTY = true
	// Every deal is closed: the live filter explains the empty list.
	r := e.run("deal", "list")
	for _, want := range []string{"filters", "live=true", "holder=" + orgID, "only the records those filters match"} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("closed-deal list lacks %q: %q", want, r.Stdout)
		}
	}
	// Another holder's actor sees none of holder A's deals: the holder filter says so.
	r = e.run(as(otherAgent, "deal", "list", "--all")...)
	if !strings.Contains(r.Stdout, "holder="+otherOrg) || !strings.Contains(r.Stdout, "filters") {
		t.Errorf("other-holder list lacks its holder filter: %q", r.Stdout)
	}
	// JSON keeps its filters exactly as before.
	e.TTY = false
	if out := e.ok("deal", "list"); fmt.Sprint(out["filters"]) != "map[holder:"+orgID+" live:true]" {
		t.Errorf("json filters %v", out["filters"])
	}
}

func TestDealCreateRefusesARepeatedParty(t *testing.T) {
	e := world(t)
	before := e.Files()
	eo := e.refused("INVALID_VALUE", 2, mkDeal("--party", elenaID)...)
	if det := details(t, eo); det["field"] != "party" || !strings.Contains(eo["hint"].(string), "deal party add") {
		t.Errorf("refused: %v", eo)
	}
	if !maps.Equal(before, e.Files()) {
		t.Error("the refused create changed files")
	}
	// The documented recipe: one party on create, the others with party add.
	e.ok(mkDeal()...)
	e.ok("deal", "party", "add", sierraDeal, elenaID, "--role", "client")
	if got := fmt.Sprint(data(t, e.ok("deal", "get", sierraDeal, "--full"))["parties"]); !strings.Contains(got, sierraID) || !strings.Contains(got, elenaID) {
		t.Errorf("parties %s", got)
	}
}
