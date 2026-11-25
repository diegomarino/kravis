package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	acmeHuman  = userID  // us_ana-acme, holder org_acme-example
	acmeAgent  = agentID // us_mail-acme, handled by Ana
	otherHuman = "us_bea-other"
	otherAgent = "us_bot-other"
	sierraID   = "org_grupo-sierra"
	msgRef     = "email:message:msg-1@example.com"
	dataRoom   = "records/interactions/2026-11/it_2026-11-23-data-room-documentation.json"
	dataRoomID = "it_2026-11-23-data-room-documentation"
)

// world is a workspace with two own organizations, each with a human and
// an agent, Elena Ruiz (one affiliation, Grupo Sierra) and Marco Silva
// (two).
func world(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.mustInit()
	e.mustAgent()
	e.ok("organization", "create", "--name", "Other Example", "--own")
	e.ok("user", "create", "bea-other", "--human", "--holder", otherOrg)
	e.ok("user", "create", "bot-other", "--agent", "--holder", otherOrg, "--handler", otherHuman)
	e.ok("organization", "create", "--name", "Grupo Sierra", "--domain", "gruposierra.example.com")
	e.ok("organization", "create", "--name", "Norte Example")
	e.ok("person", "create", "--name", "Elena Ruiz", "--email", elenaEmail)
	e.ok("person", "affiliation", "add", elenaID, sierraID, "--title", "CFO")
	e.ok("person", "create", "--name", "Marco Silva", "--email", "m.silva@example.com")
	e.ok("person", "affiliation", "add", "pe_marco-silva", sierraID)
	e.ok("person", "affiliation", "add", "pe_marco-silva", "org_norte-example")
	return e
}

// as runs a command as an actor.
func as(actor string, args ...string) []string { return append([]string{"--actor", actor}, args...) }

// record is the args of an interaction create as holder A's human; extra
// flags follow.
func record(subject string, extra ...string) []string {
	return slices.Concat([]string{"interaction", "create", "--channel", "email", "--occurred-at", "2026-11-23T17:02:00Z",
		"--subject", subject, "--participant", elenaID}, extra)
}

func participantsOf(d map[string]any) string { return fmt.Sprint(d["participants"]) }

func (e *env) setChannels(channels ...string) {
	e.t.Helper()
	var cfg map[string]any
	raw, err := os.ReadFile("config.json")
	must(e.t, err)
	must(e.t, json.Unmarshal(raw, &cfg))
	if channels == nil {
		channels = []string{} // an empty catalog is [], not null: null means the key is lacking
	}
	cfg["catalogs"].(map[string]any)["channels"] = channels
	raw, err = json.MarshalIndent(cfg, "", "  ")
	must(e.t, err)
	must(e.t, os.WriteFile("config.json", raw, 0o600))
}

// findings runs check and returns code -> records.
func (e *env) findings() map[string][]string {
	e.t.Helper()
	r := e.run("check")
	out := map[string][]string{}
	for _, f := range r.JSON(e.t)["data"].([]any) {
		m := f.(map[string]any)
		out[m["code"].(string)] = append(out[m["code"].(string)], m["record"].(string)+": "+m["message"].(string))
	}
	if (len(out) == 0) != (r.Code == 0) {
		e.t.Fatalf("check: code %d, findings %v", r.Code, out)
	}
	return out
}

// noActor forgets the default actor init stored, so reads are not limited.
func (e *env) noActor() {
	e.t.Helper()
	must(e.t, os.Remove(e.localConfig()))
}

func details(t *testing.T, eo map[string]any) map[string]any {
	t.Helper()
	return obj(t, eo, "details")
}

func TestWORKSPACE_INV_01_AnInteractionHasOneHolderThatIsAnOwnOrganization(t *testing.T) {
	e := world(t)
	d := outcomeOf(t, e.ok(record("Data room documentation")...), "created")
	if d["holder"] != orgID {
		t.Errorf("holder %v", d["holder"])
	}
	d = outcomeOf(t, e.ok(as(otherAgent, record("Other holder's note")...)...), "created")
	if d["holder"] != otherOrg {
		t.Errorf("holder %v", d["holder"])
	}
	// A record whose holder is not an own organization, or does not exist,
	// is reported.
	rec := readRecord(t, dataRoom)
	rec["holder"] = "org_norte-example"
	raw, _ := json.MarshalIndent(rec, "", "  ")
	must(t, os.WriteFile(dataRoom, raw, 0o600))
	got := e.findings()
	if len(got["INVALID_RECORD"]) == 0 || !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "not an own organization") {
		t.Errorf("findings %v", got)
	}
	rec["holder"] = "org_nowhere"
	raw, _ = json.MarshalIndent(rec, "", "  ")
	must(t, os.WriteFile(dataRoom, raw, 0o600))
	if got := e.findings(); len(got["DANGLING_REFERENCE"]) == 0 {
		t.Errorf("findings %v", got)
	}
}

func TestWORKSPACE_INV_02_AWriteTakesTheActorsHolderAndLinksShareIt(t *testing.T) {
	e := world(t)
	// An outbound initiator of the other holder is a link across holders.
	eo := e.refused("HOLDER_MISMATCH", 2, record("Data room documentation", "--initiated-by", otherHuman)...)
	if details(t, eo)["field"] != "initiated-by" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("HOLDER_MISMATCH", 2, as(otherAgent, record("Data room documentation", "--initiated-by", acmeHuman)...)...)
	d := outcomeOf(t, e.ok(record("Data room documentation", "--initiated-by", acmeAgent, "--fields", "initiated_by")...), "created")
	if d["initiated_by"] != acmeAgent {
		t.Errorf("initiated_by %v", d["initiated_by"])
	}
	// The same on update.
	e.refused("HOLDER_MISMATCH", 2, "interaction", "update", dataRoomID, "--initiated-by", otherHuman)

	// An actor writes only its own holder's interactions: another
	// holder's is not found, with or without --all-holders, and unchanged.
	for _, flags := range [][]string{nil, {"--all-holders"}} {
		e.refused("NOT_FOUND", 3, as(otherAgent, slices.Concat([]string{"interaction", "update", dataRoomID, "--subject", "Taken over"}, flags)...)...)
		e.refused("NOT_FOUND", 3, as(otherAgent, slices.Concat([]string{"interaction", "delete", dataRoomID}, flags)...)...)
		// The same for a record that does not exist at all: no difference.
		e.refused("NOT_FOUND", 3, as(otherAgent, slices.Concat([]string{"interaction", "delete", "it_2026-01-01-nothing"}, flags)...)...)
	}
	outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--subject", "Data room documentation"), "unchanged")
}

func TestWORKSPACE_INV_03_AHoldersNeverChanges(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation")...)
	e.refused("UNKNOWN_FIELD", 2, "interaction", "update", dataRoomID, "--holder", otherOrg)
	// Nothing an update can say moves it: the holder stays what it was.
	e.ok("interaction", "update", dataRoomID, "--subject", "Renamed", "--live=false")
	if h := readRecord(t, dataRoom)["holder"]; h != orgID {
		t.Errorf("holder %v", h)
	}
	e.refused("UNKNOWN_FIELD", 2, "interaction", "update", dataRoomID, "--occurred-at", "2026-12-01T10:00:00Z")
}

func TestWORKSPACE_INV_12_ReadsWithAnActorAreLimitedToItsHolder(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation")...)
	e.ok(as(otherAgent, record("Their kickoff")...)...)
	other := "it_2026-11-23-their-kickoff"

	ids := func(out map[string]any) []string {
		var got []string
		for _, r := range out["data"].([]any) {
			got = append(got, r.(map[string]any)["id"].(string))
		}
		return got
	}
	// Each actor sees its own holder's interactions, and the response says so.
	a := e.ok(as(acmeHuman, "interaction", "list")...)
	if !slices.Equal(ids(a), []string{dataRoomID}) || a["total"] != float64(1) || obj(t, a, "filters")["holder"] != orgID {
		t.Errorf("holder A: %v", a)
	}
	b := e.ok(as(otherAgent, "interaction", "list")...)
	if !slices.Equal(ids(b), []string{other}) || b["total"] != float64(1) || obj(t, b, "filters")["holder"] != otherOrg {
		t.Errorf("holder B: %v", b)
	}
	// A get of another holder's interaction is not found.
	e.refused("NOT_FOUND", 3, as(otherAgent, "interaction", "get", dataRoomID)...)
	g := e.ok(as(acmeAgent, "interaction", "get", dataRoomID)...)
	if obj(t, g, "filters")["holder"] != orgID || data(t, g)["id"] != dataRoomID {
		t.Errorf("get: %v", g)
	}
	// Without an actor, nothing is limited and nothing is stated.
	e.noActor()
	all := e.ok("interaction", "list")
	if len(ids(all)) != 2 || all["total"] != float64(2) {
		t.Errorf("no actor: %v", all)
	}
	if _, ok := obj(t, all, "filters")["holder"]; ok {
		t.Errorf("a read without an actor states no holder limit: %v", all)
	}
	if got := e.ok("interaction", "get", dataRoomID); data(t, got)["id"] != dataRoomID {
		t.Errorf("get without an actor: %v", got)
	}
}

func TestWORKSPACE_AC_09_TheHolderFilterIsStatedAndAllHoldersRemovesIt(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation")...)
	e.ok(as(otherAgent, record("Their kickoff")...)...)

	l := e.ok(as(otherAgent, "interaction", "list", "--all-holders")...)
	if l["total"] != float64(2) {
		t.Errorf("--all-holders: %v", l)
	}
	if _, ok := obj(t, l, "filters")["holder"]; ok {
		t.Errorf("--all-holders states no holder limit: %v", l)
	}
	g := e.ok(as(otherAgent, "interaction", "get", dataRoomID, "--all-holders")...)
	if _, ok := g["filters"]; ok || data(t, g)["holder"] != orgID {
		t.Errorf("get --all-holders: %v", g)
	}
	// The default actor from the local configuration is a declared actor too.
	e.ok("whoami", "--set", otherHuman)
	if l := e.ok("interaction", "list"); l["total"] != float64(1) || obj(t, l, "filters")["holder"] != otherOrg {
		t.Errorf("configured actor: %v", l)
	}
	// An actor that cannot act is refused, not ignored.
	e.refused("ACTOR_NOT_LIVE", 2, as("us_nobody", "interaction", "list")...)
}

func TestWORKSPACE_AC_13_AChannelOutsideTheCatalogIsRefusedWithTheAllowedValues(t *testing.T) {
	e := world(t)
	eo := e.refused("INVALID_VALUE", 2, "interaction", "create", "--channel", "carrier-pigeon", "--occurred-at", "2026-11-23T17:02:00Z",
		"--subject", "x", "--participant", elenaID)
	d := details(t, eo)
	if d["field"] != "channel" || fmt.Sprint(d["allowed"]) != "[email meeting call message]" {
		t.Errorf("details %v", d)
	}
	// No channel is the same refusal, with the list.
	eo = e.refused("INVALID_VALUE", 2, "interaction", "create", "--occurred-at", "2026-11-23T17:02:00Z", "--subject", "x", "--participant", elenaID)
	if fmt.Sprint(details(t, eo)["allowed"]) != "[email meeting call message]" {
		t.Errorf("details %v", eo["details"])
	}
	// The catalog is the configuration's: a workspace may edit it.
	e.setChannels("email", "carrier-pigeon")
	e.ok("interaction", "create", "--channel", "carrier-pigeon", "--occurred-at", "2026-11-23T17:02:00Z", "--subject", "x", "--participant", elenaID)
	eo = e.refused("INVALID_VALUE", 2, "interaction", "create", "--channel", "meeting", "--occurred-at", "2026-11-23T17:02:00Z", "--subject", "y", "--participant", elenaID)
	if fmt.Sprint(details(t, eo)["allowed"]) != "[email carrier-pigeon]" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("INVALID_VALUE", 2, "interaction", "update", "it_2026-11-23-x", "--channel", "meeting")
}

func TestWORKSPACE_INV_05_ARemovedChannelStaysAndTheNextUpdateMustReplaceIt(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation")...)
	e.setChannels("meeting", "call", "message") // email is gone

	// The record keeps it, and check reports it.
	if c := readRecord(t, dataRoom)["channel"]; c != "email" {
		t.Errorf("channel %v", c)
	}
	got := e.findings()
	if len(got) != 1 || len(got["VALUE_NOT_IN_CATALOG"]) != 1 || !strings.Contains(got["VALUE_NOT_IN_CATALOG"][0], orgID) {
		t.Errorf("findings %v", got)
	}
	// An unrelated update is refused, whatever it changes, and writes nothing.
	for _, args := range [][]string{{"--subject", "New subject"}, {"--live=false"}, {"--participant", "pe_marco-silva"}} {
		eo := e.refused("INVALID_VALUE", 2, slices.Concat([]string{"interaction", "update", dataRoomID}, args)...)
		d := details(t, eo)
		if d["field"] != "channel" || fmt.Sprint(d["allowed"]) != "[meeting call message]" {
			t.Errorf("details %v", d)
		}
	}
	// A channel outside the catalog does not replace it.
	e.refused("INVALID_VALUE", 2, "interaction", "update", dataRoomID, "--channel", "email")
	// Replacing it works, together with other changes.
	d := outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--channel", "meeting", "--subject", "Data room documentation"), "updated")
	if d["channel"] != "meeting" {
		t.Errorf("updated: %v", d)
	}
	if got := e.findings(); len(got) != 0 {
		t.Errorf("findings %v", got)
	}
	// An interaction with a removed channel can still be listed, read and deleted.
	e.setChannels("call")
	e.ok("interaction", "list", "--channel", "meeting")
	e.ok("interaction", "get", dataRoomID)
	outcomeOf(t, e.ok("interaction", "delete", dataRoomID), "deleted")
}

func TestINTERACTIONS_INV_01_AtLeastOneParticipantEachAPersonOrAnOrganization(t *testing.T) {
	e := world(t)
	create := func(extra ...string) []string {
		return slices.Concat([]string{"interaction", "create", "--channel", "call", "--occurred-at", "2026-11-23T17:02:00Z", "--subject", "Intro"}, extra)
	}
	eo := e.refused("INVALID_VALUE", 2, create()...)
	if details(t, eo)["field"] != "participant" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("INVALID_VALUE", 2, create("--participant", acmeHuman)...) // a user is neither
	e.refused("INVALID_VALUE", 2, create("--participant", "somebody")...)
	e.refused("NOT_FOUND", 3, create("--participant", "pe_nobody")...)
	e.refused("NOT_FOUND", 3, create("--participant", "nobody@example.com")...)
	e.refused("NOT_FOUND", 3, create("--participant", "org_nowhere")...)

	// A person by ID or email, an organization by ID; repeated ones count once.
	d := outcomeOf(t, e.ok(create("--participant", "m.silva@example.com", "--participant", "pe_marco-silva", "--participant", "org_norte-example")...), "created")
	if participantsOf(d) != "[org_norte-example pe_marco-silva]" {
		t.Errorf("participants %v", d["participants"])
	}
	id := d["id"].(string)
	// The holder's own users are implied: there is no need to list them. An
	// own organization can be a participant like any other.
	d = outcomeOf(t, e.ok("interaction", "update", id, "--participant", orgID), "updated")
	if participantsOf(d) != "[org_acme-example org_norte-example pe_marco-silva]" {
		t.Errorf("participants %v", d["participants"])
	}
	// The last participant stays.
	e.refused("INVALID_VALUE", 2, "interaction", "update", id, "--remove-participant", "pe_marco-silva",
		"--remove-participant", "org_norte-example", "--remove-participant", orgID)
	d = outcomeOf(t, e.ok("interaction", "update", id, "--remove-participant", "m.silva@example.com", "--remove-participant", orgID), "updated")
	if participantsOf(d) != "[org_norte-example]" {
		t.Errorf("participants %v", d["participants"])
	}
	// A participant stored by hand that is neither is reported.
	rec := readRecord(t, "records/interactions/2026-11/"+id+".json")
	rec["participants"] = []string{"us_ana-acme"}
	raw, _ := json.MarshalIndent(rec, "", "  ")
	must(t, os.WriteFile("records/interactions/2026-11/"+id+".json", raw, 0o600))
	if got := e.findings(); len(got["INVALID_RECORD"]) == 0 {
		t.Errorf("findings %v", got)
	}
}

func TestINTERACTIONS_INV_02_TheDirectionIsOutboundUnlessGiven(t *testing.T) {
	e := world(t)
	d := outcomeOf(t, e.ok(record("Data room documentation")...), "created")
	if d["direction"] != "outbound" {
		t.Errorf("direction %v", d["direction"])
	}
	d = outcomeOf(t, e.ok(record("Their question", "--direction", "inbound")...), "created")
	if d["direction"] != "inbound" {
		t.Errorf("direction %v", d["direction"])
	}
	eo := e.refused("INVALID_VALUE", 2, record("Sideways", "--direction", "sideways")...)
	if fmt.Sprint(details(t, eo)["allowed"]) != "[outbound inbound]" {
		t.Errorf("details %v", eo["details"])
	}
	e.refused("INVALID_VALUE", 2, "interaction", "update", dataRoomID, "--direction", "sideways")
	d = outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--direction", "inbound"), "updated")
	if d["direction"] != "inbound" {
		t.Errorf("direction %v", d["direction"])
	}
}

func TestINTERACTIONS_INV_03_TheInitiatorFollowsTheDirection(t *testing.T) {
	e := world(t)
	// Never inferred from the actor: left out, it stays absent.
	d := outcomeOf(t, e.ok(as(acmeAgent, record("Data room documentation", "--fields", "initiated_by")...)...), "created")
	if _, ok := d["initiated_by"]; ok {
		t.Errorf("an omitted initiator stays absent: %v", d)
	}
	if _, ok := readRecord(t, dataRoom)["initiated_by"]; ok {
		t.Error("the initiator was inferred")
	}

	// Outbound: a user of the holder, any of its users (a human or an agent).
	d = outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--initiated-by", acmeHuman, "--fields", "initiated_by"), "updated")
	if d["initiated_by"] != acmeHuman {
		t.Errorf("initiated_by %v", d["initiated_by"])
	}
	e.refused("INVALID_VALUE", 2, "interaction", "update", dataRoomID, "--initiated-by", elenaID) // a participant is not a user
	e.refused("NOT_FOUND", 3, "interaction", "update", dataRoomID, "--initiated-by", "us_nobody")
	e.refused("HOLDER_MISMATCH", 2, "interaction", "update", dataRoomID, "--initiated-by", otherAgent)

	// Inbound: one of the participants, by ID or email, also an organization
	// that came with a person.
	in := func(who string) []string {
		return record("Their question", "--direction", "inbound", "--initiated-by", who)
	}
	e.refused("INVALID_VALUE", 2, in(acmeHuman)...) // a user cannot start an inbound one
	e.refused("INVALID_VALUE", 2, in("pe_marco-silva")...)
	e.refused("INVALID_VALUE", 2, in("org_norte-example")...)
	e.refused("NOT_FOUND", 3, in("nobody@example.com")...)
	d = outcomeOf(t, e.ok(append(in(elenaEmail), "--fields", "initiated_by")...), "created")
	if d["initiated_by"] != elenaID {
		t.Errorf("initiated_by %v", d["initiated_by"])
	}
	d = outcomeOf(t, e.ok(append(record("Their reply", "--direction", "inbound", "--initiated-by", sierraID), "--fields", "initiated_by")...), "created")
	if d["initiated_by"] != sierraID {
		t.Errorf("initiated_by %v", d["initiated_by"])
	}

	// The direction and the initiator stay consistent on update.
	id := "it_2026-11-23-their-question"
	e.refused("INVALID_VALUE", 2, "interaction", "update", id, "--direction", "outbound")
	e.refused("INVALID_VALUE", 2, "interaction", "update", id, "--remove-participant", elenaID)
	outcomeOf(t, e.ok("interaction", "update", id, "--direction", "outbound", "--initiated-by", acmeAgent), "updated")
	// An empty value clears it.
	d = outcomeOf(t, e.ok("interaction", "update", id, "--initiated-by", "", "--fields", "initiated_by"), "updated")
	if _, ok := d["initiated_by"]; ok {
		t.Errorf("cleared: %v", d)
	}
	// Check reports an initiator that is gone, of another holder, or that does not fit.
	rec := readRecord(t, dataRoom)
	path := func(r map[string]any, p string) {
		raw, _ := json.MarshalIndent(r, "", "  ")
		must(t, os.WriteFile(p, raw, 0o600))
	}
	rec["initiated_by"] = otherHuman
	path(rec, dataRoom)
	if got := e.findings(); !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "works for "+otherOrg) {
		t.Errorf("findings %v", got)
	}
	rec["initiated_by"] = "us_nobody"
	path(rec, dataRoom)
	if got := e.findings(); len(got["DANGLING_REFERENCE"]) != 1 {
		t.Errorf("findings %v", got)
	}
	rec["initiated_by"] = elenaID
	path(rec, dataRoom)
	if got := e.findings(); len(got["INVALID_RECORD"]) != 1 {
		t.Errorf("findings %v", got)
	}
}

func TestINTERACTIONS_INV_04_APersonWithOneAffiliationAddsItsOrganization(t *testing.T) {
	e := world(t)
	// One affiliation: the organization comes with the person.
	out := e.ok(record("Data room documentation")...)
	if participantsOf(data(t, out)) != "["+sierraID+" "+elenaID+"]" || len(warningCodes(out)) != 0 {
		t.Errorf("one affiliation: %v", out)
	}
	// Several: nothing is added, and the warning lists them.
	out = e.ok("interaction", "create", "--channel", "call", "--occurred-at", "2026-11-24T08:00:00Z", "--subject", "Call with Marco", "--participant", "pe_marco-silva")
	if participantsOf(data(t, out)) != "[pe_marco-silva]" || !slices.Equal(warningCodes(out), []string{"MULTIPLE_AFFILIATIONS"}) {
		t.Errorf("two affiliations: %v", out)
	}
	w := out["warnings"].([]any)[0].(map[string]any)
	if d := obj(t, w, "details"); d["person"] != "pe_marco-silva" || fmt.Sprint(d["organizations"]) != "[org_grupo-sierra org_norte-example]" {
		t.Errorf("details %v", d)
	}
	// No affiliation: nothing to add, nothing to say.
	e.ok("person", "create", "--name", "Nora Lund")
	out = e.ok("interaction", "create", "--channel", "call", "--occurred-at", "2026-11-24T09:00:00Z", "--subject", "Call with Nora", "--participant", "pe_nora-lund")
	if participantsOf(data(t, out)) != "[pe_nora-lund]" || len(warningCodes(out)) != 0 {
		t.Errorf("no affiliation: %v", out)
	}
	// The same on update, and the organization chosen by hand sticks.
	id := "it_2026-11-24-call-with-marco"
	out = e.ok("interaction", "update", id, "--participant", "pe_nora-lund", "--participant", "org_norte-example")
	if participantsOf(data(t, out)) != "[org_norte-example pe_marco-silva pe_nora-lund]" || len(warningCodes(out)) != 0 {
		t.Errorf("update: %v", out)
	}
	out = e.ok("interaction", "update", id, "--participant", elenaID)
	if participantsOf(data(t, out)) != "[org_grupo-sierra org_norte-example pe_elena-ruiz pe_marco-silva pe_nora-lund]" {
		t.Errorf("update: %v", out)
	}
	out = e.ok("interaction", "update", id, "--remove-participant", "pe_nora-lund", "--participant", "pe_marco-silva")
	if len(warningCodes(out)) != 1 {
		t.Errorf("a person with several affiliations warns again on update: %v", out)
	}
}

func TestINTERACTIONS_INV_06_EveryInteractionHasOneChannelFromTheCatalog(t *testing.T) {
	e := world(t)
	for _, ch := range []string{"email", "meeting", "call", "message"} {
		e.ok("interaction", "create", "--channel", ch, "--occurred-at", "2026-11-23T17:02:00Z", "--subject", "via "+ch, "--participant", elenaID)
	}
	e.refused("INVALID_VALUE", 2, "interaction", "create", "--channel", "Email", "--occurred-at", "2026-11-23T17:02:00Z", "--subject", "x", "--participant", elenaID)
	e.refused("INVALID_VALUE", 2, "interaction", "update", "it_2026-11-23-via-call", "--channel", "")
	d := outcomeOf(t, e.ok("interaction", "update", "it_2026-11-23-via-call", "--channel", "message"), "updated")
	if d["channel"] != "message" {
		t.Errorf("channel %v", d["channel"])
	}
	// A channel that is not a slug, written by hand, is reported.
	p := "records/interactions/2026-11/it_2026-11-23-via-call.json"
	rec := readRecord(t, p)
	rec["channel"] = ""
	raw, _ := json.MarshalIndent(rec, "", "  ")
	must(t, os.WriteFile(p, raw, 0o600))
	if got := e.findings(); len(got["INVALID_RECORD"]) == 0 {
		t.Errorf("findings %v", got)
	}
}

func TestINTERACTIONS_INV_07_AnIdentifyingReferenceIdentifiesOneInteractionPerHolder(t *testing.T) {
	e := world(t)
	ref := []string{"--ref", msgRef}
	d := outcomeOf(t, e.ok(record("Data room documentation", ref...)...), "created")
	if d["id"] != dataRoomID {
		t.Fatalf("created: %v", d)
	}
	// The same item again, by the same holder: the same interaction, nothing written.
	before := e.Files()
	outcomeOf(t, e.ok(record("Data room documentation", ref...)...), "existing")
	// By a second user of the holder, a human or an agent, and with the
	// reference in another case where the ID allows no change.
	d = outcomeOf(t, e.ok(as(acmeAgent, record("Data room documentation", "--ref", "EMAIL:Message:msg-1@example.com")...)...), "existing")
	if d["id"] != dataRoomID {
		t.Errorf("existing: %v", d)
	}
	if !maps.Equal(before, e.Files()) {
		t.Error("existing wrote")
	}
	// By the other holder: a separate interaction, and nothing of the first
	// one is shown.
	out := e.ok(as(otherAgent, record("Data room documentation", ref...)...)...)
	d = outcomeOf(t, out, "created")
	if d["id"] != dataRoomID+"-2" || d["holder"] != otherOrg || len(warningCodes(out)) != 0 {
		t.Errorf("other holder: %v", out)
	}
	// ...which is the other holder's own existing interaction from then on.
	if d := outcomeOf(t, e.ok(as(otherHuman, record("Data room documentation", ref...)...)...), "existing"); d["holder"] != otherOrg {
		t.Errorf("%v", d)
	}
	// A second interaction of the same holder with the same subject gets a
	// suffix and says why.
	out = e.ok(record("Data room documentation", "--ref", "email:message:msg-2@example.com")...)
	if data(t, out)["id"] != dataRoomID+"-3" || !slices.Equal(warningCodes(out), []string{"SIMILAR_EXISTS"}) {
		t.Errorf("suffix: %v", out)
	}

	// What a create sent must not contradict what it found.
	for field, args := range map[string][]string{
		"channel":     {"--channel", "meeting", "--occurred-at", "2026-11-23T17:02:00Z", "--participant", elenaID},
		"occurred-at": {"--channel", "email", "--occurred-at", "2026-11-23T17:03:00Z", "--participant", elenaID},
		"participant": {"--channel", "email", "--occurred-at", "2026-11-23T17:02:00Z", "--participant", "pe_marco-silva"},
		"direction":   {"--channel", "email", "--occurred-at", "2026-11-23T17:02:00Z", "--participant", elenaID, "--direction", "inbound"},
		"initiated-by": {"--channel", "email", "--occurred-at", "2026-11-23T17:02:00Z", "--participant", elenaID,
			"--initiated-by", acmeHuman},
		"ref": {"--channel", "email", "--occurred-at", "2026-11-23T17:02:00Z", "--participant", elenaID, "--ref", "web:url:https://example.com/x"},
	} {
		eo := e.refused("DEDUP_CONFLICT", 4, slices.Concat([]string{"interaction", "create", "--subject", "Data room documentation", "--ref", msgRef}, args)...)
		if d := details(t, eo); d["id"] != dataRoomID || d["field"] != field {
			t.Errorf("%s: details %v", field, d)
		}
	}

	// Identifying references split across two interactions are a conflict.
	e.ok(record("Other thread", "--ref", "email:message:msg-3@example.com", "--ref", "granola:meeting:m-1")...)
	eo := e.refused("DEDUP_CONFLICT", 4, record("Joined", "--ref", msgRef, "--ref", "email:message:msg-3@example.com")...)
	if d := details(t, eo); d["field"] != "ref" || len(d["interactions"].([]any)) != 2 {
		t.Errorf("split: %v", d)
	}
	// An update cannot move an identifying reference onto a second interaction either.
	eo = e.refused("DEDUP_CONFLICT", 4, "interaction", "update", "it_2026-11-23-other-thread", "--ref", msgRef)
	if details(t, eo)["id"] != dataRoomID {
		t.Errorf("update: %v", eo["details"])
	}
	// Keys follow updates: a removed reference frees its key, and an added one takes it.
	outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--remove-ref", msgRef), "updated")
	outcomeOf(t, e.ok("interaction", "update", "it_2026-11-23-other-thread", "--ref", msgRef), "updated")
	outcomeOf(t, e.ok(record("Whatever", "--ref", msgRef)...), "existing")
	if got := e.findings(); len(got) != 0 {
		t.Errorf("findings %v", got)
	}
	// Deleting an interaction frees its references.
	outcomeOf(t, e.ok("interaction", "delete", "it_2026-11-23-other-thread"), "deleted")
	outcomeOf(t, e.ok(record("Again", "--ref", msgRef)...), "created")
	if got := e.findings(); len(got) != 0 {
		t.Errorf("findings %v", got)
	}
}

func TestINTERACTIONS_INV_07_ACrashedCreateIsCompletedAndRacesAreReported(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation", "--ref", msgRef)...)
	// Break the key by hand: the record keeps the reference, the key is gone.
	keys, _ := filepath.Glob("keys/interaction-ref/*/*.json")
	if len(keys) != 1 {
		t.Fatalf("keys %v", keys)
	}
	must(t, os.Remove(keys[0]))
	if got := e.findings(); len(got["VALUE_WITHOUT_KEY"]) != 1 {
		t.Errorf("findings %v", got)
	}
	// A key whose record is gone, or that the record lost, is reported too.
	must(t, os.WriteFile(keys[0], []byte(fmt.Sprintf("{\"key\":\"interaction|%s|%s\",\"record\":\"it_2026-01-01-gone\"}\n", orgID, msgRef)), 0o600))
	if got := e.findings(); len(got["KEY_WITHOUT_VALUE"]) != 0 && len(got["VALUE_WITHOUT_KEY"]) == 0 {
		t.Errorf("findings %v", got)
	}
}

func TestINTERACTIONS_INV_08_OnlyReferencesAndSummariesAreKept(t *testing.T) {
	e := world(t)
	out := e.runWithStdin("The CFO asked for the data room index.\n", append(record("Data room documentation", "--ref", msgRef, "--ref", "gmail:message:18f3a@Example.COM"), "--summary-file", "-")...)
	if out.Code != 0 {
		t.Fatalf("create: %q", out.Stderr)
	}
	rec := readRecord(t, dataRoom)
	want := []string{"id", "holder", "occurred_at", "channel", "direction", "subject", "participants", "references", "live", "version",
		"created_at", "created_by", "updated_at", "updated_by"}
	got := keysOf(rec)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("fields %v, want %v", got, want)
	}
	refs := rec["references"].([]any)
	if fmt.Sprint(refs[1]) != "map[id:18f3a kind:message scope:example.com source:gmail]" ||
		fmt.Sprint(refs[0]) != "map[id:msg-1@example.com kind:message source:email]" {
		t.Errorf("references %v", refs)
	}
	text, err := os.ReadFile(strings.TrimSuffix(dataRoom, ".json") + ".md")
	must(t, err)
	if string(text) != "The CFO asked for the data room index." {
		t.Errorf("sidecar %q", text)
	}
	// The summary is read on request, never in the compact view.
	if _, ok := data(t, e.ok("interaction", "get", dataRoomID))["summary"]; ok {
		t.Error("summary in the compact view")
	}
	if s := data(t, e.ok("interaction", "get", dataRoomID, "--fields", "summary"))["summary"]; s != "The CFO asked for the data room index." {
		t.Errorf("summary %v", s)
	}
	if s := data(t, e.ok("interaction", "get", dataRoomID, "--full"))["summary"]; s == nil {
		t.Error("--full shows the summary")
	}
	l := e.ok("interaction", "list", "--fields", "summary")["data"].([]any)
	if l[0].(map[string]any)["summary"] == nil {
		t.Errorf("list --fields summary: %v", l)
	}
	// An interaction with no summary has no sidecar.
	e.ok(record("No summary")...)
	if _, err := os.Stat("records/interactions/2026-11/it_2026-11-23-no-summary.md"); !os.IsNotExist(err) {
		t.Error("an empty summary left a sidecar")
	}
}

func TestSummaryUpdatesOnlyWhenItChanges(t *testing.T) {
	e := world(t)
	file := filepath.Join(t.TempDir(), "summary.md")
	must(t, os.WriteFile(file, []byte("First.\n"), 0o600))
	e.ok(record("Data room documentation", "--summary-file", file)...)
	outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--summary-file", file), "unchanged")
	// A write that touched the summary shows it, without asking.
	must(t, os.WriteFile(file, []byte("First, edited.\n"), 0o600))
	if d := outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--summary-file", file), "updated"); d["summary"] != "First, edited.\n" {
		t.Errorf("updated: %v", d)
	}
	if d := outcomeOf(t, e.ok(record("With summary", "--summary-file", file)...), "created"); d["summary"] != "First, edited.\n" {
		t.Errorf("created: %v", d)
	}
	e.ok("interaction", "delete", "it_2026-11-23-with-summary")
	must(t, os.WriteFile(file, []byte("First.\n"), 0o600))
	e.ok("interaction", "update", dataRoomID, "--summary-file", file)
	must(t, os.WriteFile(file, []byte("Second.\n"), 0o600))
	d := outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--summary-file", file, "--fields", "version,summary"), "updated")
	if d["summary"] != "Second.\n" {
		t.Errorf("updated: %v", d)
	}
	d = outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--summary-file", "", "--fields", "summary"), "updated")
	if _, ok := d["summary"]; ok {
		t.Errorf("cleared: %v", d)
	}
	if _, err := os.Stat(strings.TrimSuffix(dataRoom, ".json") + ".md"); !os.IsNotExist(err) {
		t.Error("a cleared summary left a sidecar")
	}
	e.refused("INVALID_VALUE", 2, "interaction", "update", dataRoomID, "--summary-file", filepath.Join(t.TempDir(), "missing.md"))
	// Deleting returns the summary it had, on request.
	e.ok("interaction", "update", dataRoomID, "--summary-file", file)
	d = outcomeOf(t, e.ok("interaction", "delete", dataRoomID, "--fields", "summary"), "deleted")
	if d["summary"] != "Second.\n" {
		t.Errorf("deleted: %v", d)
	}
	if _, err := os.Stat(strings.TrimSuffix(dataRoom, ".json") + ".md"); !os.IsNotExist(err) {
		t.Error("a delete left a sidecar")
	}
}

func TestINTERACTIONS_INV_09_AnUnknownSourceIsAcceptedWithAWarningAndMatchedExactly(t *testing.T) {
	e := world(t)
	out := e.ok(record("Data room documentation", "--ref", "Notion:Page:Abc-123")...)
	outcomeOf(t, out, "created")
	if !slices.Equal(warningCodes(out), []string{"UNKNOWN_SOURCE"}) {
		t.Errorf("warnings %v", out["warnings"])
	}
	if det := obj(t, out["warnings"].([]any)[0].(map[string]any), "details"); det["ref"] != "notion:page:Abc-123" || det["source"] != "notion:page" {
		t.Errorf("details %v", det)
	}
	if refs := readRecord(t, dataRoom)["references"]; fmt.Sprint(refs) != "[map[id:Abc-123 kind:page source:notion]]" {
		t.Errorf("references %v", refs)
	}
	// It identifies by exact equality: the source and the kind are lowercased, the ID is not.
	out = e.ok(record("Data room documentation", "--ref", "notion:page:Abc-123")...)
	outcomeOf(t, out, "existing")
	if !slices.Equal(warningCodes(out), []string{"UNKNOWN_SOURCE"}) {
		t.Errorf("an existing result keeps the warnings: %v", out)
	}
	out = e.ok(record("Data room documentation", "--ref", "notion:page:abc-123")...)
	if d := outcomeOf(t, out, "created"); d["id"] == dataRoomID {
		t.Errorf("another ID is another item: %v", d)
	}
	// A known source with a kind it does not have is unknown too, and its shape is not judged.
	out = e.ok(record("Another one", "--ref", "email:thread:t-1@@x")...)
	if !slices.Equal(warningCodes(out), []string{"UNKNOWN_SOURCE"}) {
		t.Errorf("warnings %v", out)
	}
	// Also on update, once per reference.
	out = e.ok("interaction", "update", "it_2026-11-23-another-one", "--ref", "notion:page:x", "--ref", "notion:page:x")
	if !slices.Equal(warningCodes(out), []string{"UNKNOWN_SOURCE"}) {
		t.Errorf("warnings %v", out)
	}
}

// TestSourceCatalog has a case per row of the catalog in docs/storage.md.
func TestSourceCatalog(t *testing.T) {
	tests := []struct {
		name, ref  string
		canonical  string // the stored form
		identifies bool
	}{
		{"email:message", "Email:Message:AbC.1@Example.com", "email:message:AbC.1@Example.com", true},
		{"ical:event", "ICAL:event:uid-1@example.com", "ical:event:uid-1@example.com", true},
		{"ical:event occurrence", "ical:event:uid-1@example.com/2026-11-23T17:00:00Z", "ical:event:uid-1@example.com/2026-11-23T17:00:00Z", true},
		{"granola:meeting", "granola:meeting:3f2a", "granola:meeting:3f2a", true},
		{"slack:message", "slack:message:C024BE91L/1700000000.000100", "slack:message:C024BE91L/1700000000.000100", true},
		{"telegram:message", "telegram:message:-100123/456", "telegram:message:-100123/456", true},
		{"gmail:message", "Gmail:Message:18f3a@Example.COM", "gmail:message:18f3a@example.com", false},
		{"gmail:thread", "gmail:thread:18f00@example.com", "gmail:thread:18f00@example.com", false},
		{"outlook:message", "outlook:message:AAMk=@example.com", "outlook:message:AAMk=@example.com", false},
		{"drive:file", "drive:file:1AbC", "drive:file:1AbC", false},
		{"onedrive:file", "onedrive:file:01XYZ", "onedrive:file:01XYZ", false},
		{"web:url", "web:url:HTTPS://Example.COM/Path?q=1#frag", "web:url:https://example.com/Path?q=1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := world(t)
			out := e.ok(record("Data room documentation", "--ref", tt.ref)...)
			if len(warningCodes(out)) != 0 {
				t.Errorf("a catalog source warns: %v", out["warnings"])
			}
			refs := readRecord(t, dataRoom)["references"].([]any)
			r := refs[0].(map[string]any)
			stored := fmt.Sprintf("%v:%v:%v", r["source"], r["kind"], r["id"])
			if r["scope"] != nil {
				stored += "@" + r["scope"].(string)
			}
			if stored != tt.canonical {
				t.Errorf("stored %q, want %q", stored, tt.canonical)
			}
			// Whether it identifies is whether the same reference finds the
			// interaction again, in its given or canonical form.
			want := "created"
			if tt.identifies {
				want = "existing"
			}
			for _, ref := range []string{tt.ref, tt.canonical} {
				out := e.ok(record("Data room documentation", "--ref", ref)...)
				if out["outcome"] != want {
					t.Errorf("%s again: outcome %v, want %s", ref, out["outcome"], want)
				}
			}
			keys, _ := filepath.Glob("keys/interaction-ref/*/*.json")
			if (len(keys) == 1) != tt.identifies {
				t.Errorf("%d keys, identifies %v", len(keys), tt.identifies)
			}
			// A reference is removed by any of its spellings.
			e.ok("interaction", "update", dataRoomID, "--remove-ref", tt.ref)
			if refs := readRecord(t, dataRoom)["references"]; refs != nil {
				t.Errorf("references after remove: %v", refs)
			}
			if keys, _ := filepath.Glob("keys/interaction-ref/*/*.json"); len(keys) != 0 {
				t.Errorf("keys after remove: %v", keys)
			}
		})
	}
}

func TestSourceReferencesAreValidated(t *testing.T) {
	e := world(t)
	for name, tt := range map[string]struct{ ref, why string }{
		"a scope is required":      {"gmail:message:18f3a", "needs its account"},
		"an empty scope":           {"gmail:thread:18f3a@", "needs its account"},
		"an empty ID with a scope": {"outlook:message:@example.com", "needs its account"},
		"a scope is refused":       {"granola:meeting:3f2a@example.com", "takes no scope"},
		"a scope on drive":         {"drive:file:1AbC@x", "takes no scope"},
		"no kind":                  {"email:abc", "not a source reference"},
		"no ID":                    {"email:message:", "not a source reference"},
		"no source":                {":message:abc", "not a source reference"},
		"nothing":                  {"", "not a source reference"},
		"slack without a channel":  {"slack:message:123.456", "not <channel ID>/<ts>"},
		"slack with a third part":  {"slack:message:C123/123.456/extra", "not <channel ID>/<ts>"},
		"telegram doubled slash":   {"telegram:message:-100123//42", "not <chat ID>/<message ID>"},
		"telegram without a chat":  {"telegram:message:42", "not <chat ID>/<message ID>"},
		"a URL without a host":     {"web:url:/just/a/path", "not a URL"},
		"a URL without a scheme":   {"web:url:example.com/x", "not a URL"},
	} {
		t.Run(name, func(t *testing.T) {
			eo := e.refused("INVALID_VALUE", 2, record("Data room documentation", "--ref", tt.ref)...)
			if details(t, eo)["field"] != "ref" || !strings.Contains(eo["message"].(string), tt.why) {
				t.Errorf("error %v", eo)
			}
			e.ok(record("Valid one")...)
			e.refused("INVALID_VALUE", 2, "interaction", "update", "it_2026-11-23-valid-one", "--ref", tt.ref)
			e.refused("INVALID_VALUE", 2, "interaction", "update", "it_2026-11-23-valid-one", "--remove-ref", tt.ref)
			e.ok("interaction", "delete", "it_2026-11-23-valid-one")
		})
	}
	// A reference stored by hand that is not in its canonical form is reported.
	e.ok(record("Data room documentation", "--ref", "gmail:message:18f3a@example.com")...)
	rec := readRecord(t, dataRoom)
	rec["references"] = []map[string]string{{"source": "Gmail", "kind": "message", "id": "18f3a"}}
	raw, _ := json.MarshalIndent(rec, "", "  ")
	must(t, os.WriteFile(dataRoom, raw, 0o600))
	if got := e.findings(); len(got["INVALID_RECORD"]) == 0 {
		t.Errorf("findings %v", got)
	}
}

func TestInteractionIDsAndFoldersFollowTheWorkspaceTimezone(t *testing.T) {
	e := world(t) // Europe/Madrid
	// 23:30 UTC is already the next day in Madrid.
	d := outcomeOf(t, e.ok("interaction", "create", "--channel", "call", "--occurred-at", "2026-11-23T23:30:00Z",
		"--subject", "Late call", "--participant", elenaID, "--fields", "occurred_at"), "created")
	if d["id"] != "it_2026-11-24-late-call" || d["occurred_at"] != "2026-11-23T23:30:00Z" {
		t.Errorf("created: %v", d)
	}
	if _, err := os.Stat("records/interactions/2026-11/it_2026-11-24-late-call.json"); err != nil {
		t.Error(err)
	}
	// An offset is converted: the instant is stored in UTC. The last day of
	// a month, late, belongs to the next month's folder.
	d = outcomeOf(t, e.ok("interaction", "create", "--channel", "call", "--occurred-at", "2026-11-30T23:30:00+00:00",
		"--subject", "Month end", "--participant", elenaID, "--fields", "occurred_at"), "created")
	if d["id"] != "it_2026-12-01-month-end" {
		t.Errorf("created: %v", d)
	}
	if _, err := os.Stat("records/interactions/2026-12/it_2026-12-01-month-end.json"); err != nil {
		t.Error(err)
	}
	d = outcomeOf(t, e.ok("interaction", "create", "--channel", "call", "--occurred-at", "2026-11-23T10:00:00-05:00",
		"--subject", "Té con Peña: ¿qué tal? "+strings.Repeat("largo ", 12), "--participant", elenaID, "--fields", "occurred_at"), "created")
	if d["occurred_at"] != "2026-11-23T15:00:00Z" || !strings.HasPrefix(d["id"].(string), "it_2026-11-23-te-con-pena-que-tal-largo-largo") ||
		len(d["id"].(string)) > len("it_2026-11-23-")+40 {
		t.Errorf("created: %v", d)
	}
	// A subject with no ASCII form gives the date alone.
	d = outcomeOf(t, e.ok("interaction", "create", "--channel", "call", "--occurred-at", "2026-11-22T10:00:00Z",
		"--subject", "会議", "--participant", elenaID), "created")
	if d["id"] != "it_2026-11-22" {
		t.Errorf("created: %v", d)
	}
	// Another timezone moves the day, for new records only.
	cfg := readRecord(t, "config.json")
	cfg["timezone"] = "America/Los_Angeles"
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	must(t, os.WriteFile("config.json", raw, 0o600))
	d = outcomeOf(t, e.ok("interaction", "create", "--channel", "call", "--occurred-at", "2026-11-24T02:00:00Z",
		"--subject", "Evening call", "--participant", elenaID), "created")
	if d["id"] != "it_2026-11-23-evening-call" {
		t.Errorf("created: %v", d)
	}
	e.ok("interaction", "get", "it_2026-11-24-late-call")
	if got := e.findings(); len(got) != 0 {
		t.Errorf("findings %v", got)
	}
}

func TestInteractionFileAsStored(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation", "--ref", msgRef, "--initiated-by", acmeHuman)...)
	raw, err := os.ReadFile(dataRoom)
	must(t, err)
	want := `{
  "id": "it_2026-11-23-data-room-documentation",
  "holder": "org_acme-example",
  "occurred_at": "2026-11-23T17:02:00Z",
  "channel": "email",
  "direction": "outbound",
  "subject": "Data room documentation",
  "participants": [
    "org_grupo-sierra",
    "pe_elena-ruiz"
  ],
  "initiated_by": "us_ana-acme",
  "references": [
    {
      "source": "email",
      "kind": "message",
      "id": "msg-1@example.com"
    }
  ],
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
	// The compact view and the full record.
	d := data(t, e.ok("interaction", "get", dataRoomID))
	if fmt.Sprint(keysOf(d)) != "[channel direction holder id live occurred_at participants subject]" {
		t.Errorf("compact view %v", keysOf(d))
	}
	if _, ok := data(t, e.ok("interaction", "get", dataRoomID, "--full"))["references"]; !ok {
		t.Error("--full shows the references")
	}
	e.refused("UNKNOWN_FIELD", 2, "interaction", "get", dataRoomID, "--fields", "nonsense")
	// Unknown fields on disk survive a rewrite.
	rec := readRecord(t, dataRoom)
	rec["x_note"] = "kept"
	out, _ := json.MarshalIndent(rec, "", "  ")
	must(t, os.WriteFile(dataRoom, out, 0o600))
	e.ok("interaction", "update", dataRoomID, "--subject", "Data room documentation (v2)")
	if readRecord(t, dataRoom)["x_note"] != "kept" {
		t.Error("an unknown field was lost")
	}
}

func TestWORKSPACE_AC_04_InteractionWritesReturnAnOutcomeAndTheCompactView(t *testing.T) {
	e := world(t)
	d := outcomeOf(t, e.ok(record("Data room documentation", "--ref", msgRef, "--fields", "version")...), "created")
	if d["id"] != dataRoomID || d["live"] != true || d["subject"] != "Data room documentation" || d["version"] != float64(1) ||
		d["channel"] != "email" || d["occurred_at"] != "2026-11-23T17:02:00Z" || d["holder"] != orgID {
		t.Errorf("created: %v", d)
	}
	before := e.Files()
	outcomeOf(t, e.ok(record("Data room documentation", "--ref", msgRef)...), "existing")
	if !maps.Equal(before, e.Files()) {
		t.Error("existing wrote")
	}
	d = outcomeOf(t, e.ok("interaction", "update", dataRoomID, "--subject", "Data room index", "--fields", "version"), "updated")
	if d["subject"] != "Data room index" || d["version"] != float64(2) || d["id"] != dataRoomID {
		t.Errorf("updated: %v", d)
	}
	before = e.Files()
	for _, args := range [][]string{{"--subject", "Data room index"}, {"--participant", elenaID}, {"--ref", msgRef}, {"--remove-ref", "web:url:https://example.com/"},
		{"--remove-participant", "pe_marco-silva"}, {"--live"}, {"--initiated-by", ""}, {"--channel", "email"}} {
		d = outcomeOf(t, e.ok(slices.Concat([]string{"interaction", "update", dataRoomID, "--fields", "version"}, args)...), "unchanged")
		if d["version"] != float64(2) {
			t.Errorf("unchanged bumped the version: %v", d)
		}
	}
	if !maps.Equal(before, e.Files()) {
		t.Error("unchanged wrote")
	}
	d = outcomeOf(t, e.ok("interaction", "delete", dataRoomID), "deleted")
	if d["id"] != dataRoomID || d["subject"] != "Data room index" {
		t.Errorf("deleted: %v", d)
	}
	if _, err := os.Stat(dataRoom); !os.IsNotExist(err) {
		t.Error("the record is still there")
	}
	if keys, _ := filepath.Glob("keys/interaction-ref/*/*.json"); len(keys) != 0 {
		t.Errorf("keys left: %v", keys)
	}
	e.refused("NOT_FOUND", 3, "interaction", "delete", dataRoomID)
	e.refused("NOT_FOUND", 3, "interaction", "get", "nonsense")
}

func TestWORKSPACE_AC_05_InteractionListsPageNewestFirstWithATotalAndACursor(t *testing.T) {
	e := world(t)
	for i, at := range []string{"2026-11-20T10:00:00Z", "2026-11-22T10:00:00Z", "2026-11-22T10:00:00Z", "2026-11-21T10:00:00Z", "2026-11-23T10:00:00Z"} {
		e.ok("interaction", "create", "--channel", "call", "--occurred-at", at, "--subject", fmt.Sprintf("Call %d", i), "--participant", elenaID)
	}
	e.ok(as(otherAgent, "interaction", "create", "--channel", "call", "--occurred-at", "2026-11-24T10:00:00Z", "--subject", "Theirs", "--participant", elenaID)...)
	e.ok("interaction", "update", "it_2026-11-21-call-3", "--live=false")
	e.noActor()

	order := func(out map[string]any) string {
		var ids []string
		for _, r := range out["data"].([]any) {
			ids = append(ids, strings.TrimPrefix(r.(map[string]any)["id"].(string), "it_2026-11-"))
		}
		return strings.Join(ids, " ")
	}
	// Newest first, the ID breaking ties; live ones; all holders without an actor.
	l := e.ok("interaction", "list")
	if order(l) != "24-theirs 23-call-4 22-call-1 22-call-2 20-call-0" || l["total"] != float64(5) || l["next_cursor"] != nil {
		t.Errorf("list: %v", l)
	}
	if l := e.ok("interaction", "list", "--all"); order(l) != "24-theirs 23-call-4 22-call-1 22-call-2 21-call-3 20-call-0" {
		t.Errorf("--all: %v", order(l))
	}
	// Pages of two for the holder: the cursor keeps the filters, and a cursor from another list is refused.
	p1 := e.ok(as(acmeHuman, "interaction", "list", "--limit", "2")...)
	if order(p1) != "23-call-4 22-call-1" || p1["total"] != float64(4) || p1["next_cursor"] == nil {
		t.Errorf("page 1: %v", p1)
	}
	p2 := e.ok(as(acmeHuman, "interaction", "list", "--limit", "2", "--cursor", p1["next_cursor"].(string))...)
	if order(p2) != "22-call-2 20-call-0" || p2["next_cursor"] != nil {
		t.Errorf("page 2: %v", p2)
	}
	for _, args := range [][]string{{"--all"}, {"--direction", "inbound"}, {"--all-holders"}} {
		eo := e.refused("INVALID_VALUE", 2, slices.Concat(as(acmeHuman, "interaction", "list", "--limit", "2", "--cursor", p1["next_cursor"].(string)), args)...)
		if details(t, eo)["field"] != "cursor" {
			t.Errorf("details %v", eo["details"])
		}
	}
	e.refused("INVALID_VALUE", 2, "interaction", "list", "--limit", "-1")
	e.refused("INVALID_VALUE", 2, "interaction", "list", "--limit", "0", "--cursor", p1["next_cursor"].(string))
	if l := e.ok("interaction", "list", "--limit", "0"); len(l["data"].([]any)) != 5 {
		t.Errorf("--limit 0: %v", l)
	}
}

func TestInteractionListFilters(t *testing.T) {
	e := world(t)
	mk := func(actor, subject, at, channel string, extra ...string) {
		e.ok(as(actor, slices.Concat([]string{"interaction", "create", "--channel", channel, "--occurred-at", at, "--subject", subject}, extra)...)...)
	}
	mk(acmeHuman, "Ana mail", "2026-11-02T10:00:00Z", "email", "--participant", elenaID, "--initiated-by", acmeHuman)
	mk(acmeAgent, "Agent mail", "2026-11-10T10:00:00Z", "email", "--participant", elenaID, "--initiated-by", acmeAgent)
	mk(acmeHuman, "Their question", "2026-11-15T10:00:00Z", "call", "--participant", elenaID, "--direction", "inbound", "--initiated-by", elenaID)
	mk(acmeHuman, "Marco meeting", "2026-11-30T23:30:00Z", "meeting", "--participant", "pe_marco-silva")
	mk(acmeHuman, "Norte call", "2026-12-01T09:00:00Z", "call", "--participant", "org_norte-example")
	mk(otherHuman, "Beas mail", "2026-11-12T10:00:00Z", "email", "--participant", elenaID, "--initiated-by", otherHuman)

	subjects := func(args ...string) string {
		out := e.ok(slices.Concat([]string{"interaction", "list", "--all-holders"}, args)...)
		var s []string
		for _, r := range out["data"].([]any) {
			s = append(s, r.(map[string]any)["subject"].(string))
		}
		return strings.Join(s, ",")
	}
	for name, tt := range map[string]struct {
		args []string
		want string
	}{
		"participant person":   {[]string{"--participant", elenaID}, "Their question,Beas mail,Agent mail,Ana mail"},
		"participant email":    {[]string{"--participant", elenaEmail, "--direction", "inbound"}, "Their question"},
		"participant org":      {[]string{"--participant", "org_norte-example"}, "Norte call"},
		"participant org kept": {[]string{"--participant", sierraID, "--channel", "email"}, "Beas mail,Agent mail,Ana mail"},
		"direction outbound":   {[]string{"--direction", "outbound", "--channel", "call"}, "Norte call"},
		"channel":              {[]string{"--channel", "meeting"}, "Marco meeting"},
		"initiated by a user":  {[]string{"--initiated-by", acmeHuman}, "Ana mail"},
		"with agents":          {[]string{"--initiated-by", acmeHuman, "--with-agents"}, "Agent mail,Ana mail"},
		"initiated by person":  {[]string{"--initiated-by", elenaEmail}, "Their question"},
		"initiated by org":     {[]string{"--initiated-by", sierraID}, ""},
		"from a date":          {[]string{"--from", "2026-11-12"}, "Norte call,Marco meeting,Their question,Beas mail"},
		"to a date, included":  {[]string{"--to", "2026-11-10"}, "Agent mail,Ana mail"},
		"a month":              {[]string{"--from", "2026-11-01", "--to", "2026-11-30"}, "Their question,Beas mail,Agent mail,Ana mail"},
		"a day in the zone":    {[]string{"--from", "2026-12-01", "--to", "2026-12-01"}, "Norte call,Marco meeting"},
		"an instant":           {[]string{"--from", "2026-11-15T10:00:00Z", "--to", "2026-11-15T10:00:00Z"}, "Their question"},
		"an instant offset":    {[]string{"--from", "2026-11-15T11:00:00+01:00", "--to", "2026-11-15T11:00:00+01:00"}, "Their question"},
		"nothing":              {[]string{"--channel", "message"}, ""},
	} {
		if got := subjects(tt.args...); got != tt.want {
			t.Errorf("%s: %q, want %q", name, got, tt.want)
		}
	}
	// Filters apply within the holder's limit.
	out := e.ok(as(otherHuman, "interaction", "list", "--participant", elenaID)...)
	if out["total"] != float64(1) {
		t.Errorf("limited: %v", out)
	}
	// A filter that is wrong is refused.
	for name, tt := range map[string]struct {
		code string
		exit int
		args []string
	}{
		"direction":      {"INVALID_VALUE", 2, []string{"--direction", "sideways"}},
		"from":           {"INVALID_VALUE", 2, []string{"--from", "yesterday"}},
		"to":             {"INVALID_VALUE", 2, []string{"--to", "2026-13-45"}},
		"participant":    {"INVALID_VALUE", 2, []string{"--participant", "somebody"}},
		"with-agents":    {"INVALID_VALUE", 2, []string{"--with-agents"}},
		"agents of one":  {"INVALID_VALUE", 2, []string{"--initiated-by", acmeAgent, "--with-agents"}},
		"a missing user": {"NOT_FOUND", 3, []string{"--initiated-by", "us_nobody", "--with-agents"}},
	} {
		t.Run(name, func(t *testing.T) {
			e.refused(tt.code, tt.exit, slices.Concat([]string{"interaction", "list"}, tt.args)...)
		})
	}
	e.refused("NOT_FOUND", 3, "interaction", "list", "--participant", "nobody@example.com")
}

func TestWORKSPACE_AC_10_InteractionsAreReferredToAndDeleteCleanly(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation", "--initiated-by", acmeAgent)...)
	referrers := func(eo map[string]any) string { return fmt.Sprint(details(t, eo)["referrers"]) }

	// What an interaction refers to cannot be deleted while it does.
	for _, target := range [][]string{{"person", "delete", elenaID}, {"organization", "delete", sierraID}, {"user", "delete", acmeAgent}} {
		if got := referrers(e.refused("REFERENCED", 4, target...)); !strings.Contains(got, dataRoomID) {
			t.Errorf("%v referrers %s", target, got)
		}
	}
	if got := referrers(e.refused("REFERENCED", 4, "organization", "delete", orgID)); !strings.Contains(got, dataRoomID) {
		t.Errorf("holder referrers %s", got)
	}
	// A holder keeps its own mark while an interaction has it as holder, even with no users left.
	e.refused("REFERENCED", 4, "organization", "update", otherOrg, "--own=false")
	e.ok(as(otherAgent, record("Their note")...)...)
	if eo := e.refused("REFERENCED", 4, "organization", "update", otherOrg, "--own=false"); !strings.Contains(referrers(eo), "it_2026-11-23-their-note") {
		t.Errorf("own mark referrers %s", referrers(eo))
	}
	// Nothing refers to an interaction yet (tasks will), so it deletes cleanly, and what it referred to is free.
	outcomeOf(t, e.ok("interaction", "delete", dataRoomID), "deleted")
	outcomeOf(t, e.ok("user", "delete", acmeAgent), "deleted")
	e.refused("REFERENCED", 4, "person", "delete", elenaID) // still a participant of Their note
	e.ok("organization", "create", "--name", "Lonely Example")
	e.ok("interaction", "create", "--channel", "call", "--occurred-at", "2026-11-25T10:00:00Z", "--subject", "Cold call", "--participant", "org_lonely-example")
	if got := referrers(e.refused("REFERENCED", 4, "organization", "delete", "org_lonely-example")); got != "[it_2026-11-25-cold-call]" {
		t.Errorf("participant referrers %s", got)
	}
	outcomeOf(t, e.ok("interaction", "delete", "it_2026-11-25-cold-call"), "deleted")
	outcomeOf(t, e.ok("organization", "delete", "org_lonely-example"), "deleted")
}

func TestInteractionInputIsRefusedBeforeWriting(t *testing.T) {
	e := world(t)
	base := []string{"interaction", "create", "--channel", "email", "--occurred-at", "2026-11-23T17:02:00Z", "--subject", "x", "--participant", elenaID}
	without := func(flag string) []string {
		var out []string
		for i := 0; i < len(base); i++ {
			if base[i] == flag {
				i++
				continue
			}
			out = append(out, base[i])
		}
		return out
	}
	for _, flag := range []string{"--occurred-at", "--subject", "--participant"} {
		eo := e.refused("INVALID_VALUE", 2, without(flag)...)
		if details(t, eo)["field"] != strings.TrimPrefix(flag, "--") {
			t.Errorf("%s: %v", flag, eo)
		}
	}
	for name, tt := range map[string][]string{
		"a date":          {"--occurred-at", "yesterday"},
		"a date only":     {"--occurred-at", "2026-11-23"},
		"a blank subject": {"--subject", "  "},
	} {
		args := slices.Concat(base, tt)
		if eo := e.refused("INVALID_VALUE", 2, args...); eo["message"] == "" {
			t.Errorf("%s: %v", name, eo)
		}
	}
	e.refused("UNKNOWN_FIELD", 2, slices.Concat(base, []string{"--holder", otherOrg})...)
	e.refused("UNKNOWN_FIELD", 2, slices.Concat(base, []string{"--source", "gmail"})...)
	e.refused("INVALID_VALUE", 2, "interaction", "create", "extra")
	e.refused("INVALID_VALUE", 2, slices.Concat(base, []string{"--summary-file", filepath.Join(t.TempDir(), "nope.md")})...)
	e.refused("NOT_FOUND", 3, "interaction", "update", "it_2026-11-23-missing", "--subject", "x")
	e.refused("NOT_FOUND", 3, "interaction", "update", "not-an-id", "--subject", "x")
	// An actor that cannot act cannot write.
	e.refused("ACTOR_NOT_LIVE", 2, as("us_nobody", base...)...)
	e.noActor()
	e.refused("MISSING_ACTOR", 2, base...)
}

func TestCheckReportsBrokenInteractions(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation", "--ref", msgRef)...)
	if got := e.findings(); len(got) != 0 {
		t.Fatalf("a clean workspace: %v", got)
	}
	rec := readRecord(t, dataRoom)
	rec["participants"] = []string{"pe_nobody", "org_nowhere", "pe_elena-ruiz"}
	rec["direction"] = "sideways"
	rec["subject"] = ""
	rec["occurred_at"] = "yesterday"
	raw, _ := json.MarshalIndent(rec, "", "  ")
	must(t, os.WriteFile(dataRoom, raw, 0o600))
	got := e.findings()
	if len(got["DANGLING_REFERENCE"]) != 2 || len(got["INVALID_RECORD"]) != 3 {
		t.Errorf("findings %v", got)
	}
	// A record at the wrong path, or that is not an interaction ID.
	must(t, os.WriteFile(dataRoom, []byte(strings.Replace(string(raw), `"it_2026-11-23-data-room-documentation"`, `"it_2026-11-23-other"`, 1)), 0o600))
	if got := e.findings(); !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "does not match the file name") {
		t.Errorf("findings %v", got)
	}
	short := strings.Replace(string(raw), `"it_2026-11-23-data-room-documentation"`, `"it_x"`, 1)
	must(t, os.WriteFile(dataRoom, []byte(short), 0o600))
	if got := e.findings(); !strings.Contains(strings.Join(got["INVALID_RECORD"], "\n"), "not a interaction ID") {
		t.Errorf("findings %v", got)
	}
	// A reference to a record that exists only in an unreadable config is not judged.
	must(t, os.WriteFile(dataRoom, raw, 0o600))
	must(t, os.WriteFile("config.json", []byte("{nope"), 0o600))
	if got := e.findings(); len(got["VALUE_NOT_IN_CATALOG"]) != 0 || len(got["WORKSPACE_FILE_INVALID"]) != 1 {
		t.Errorf("findings %v", got)
	}
}

func TestInteractionListsOnATerminal(t *testing.T) {
	e := world(t)
	e.ok(record("Data room documentation")...)
	e.TTY = true
	r := e.run("--actor", acmeHuman, "interaction", "list")
	if r.Code != 0 || !strings.Contains(r.Stdout, dataRoomID) {
		t.Errorf("table: %d %q", r.Code, r.Stdout)
	}
}

func TestCommandGroupsWithoutASubcommandAreUsageErrors(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{{"user"}, {"person"}, {"person", "affiliation"}, {"organization"}, {"interaction"}} {
		eo := wantError(t, e.run(args...), "INVALID_VALUE", 2)
		if !strings.Contains(eo["message"].(string), "needs a subcommand") {
			t.Errorf("%v: %v", args, eo)
		}
	}
}

func TestInteractionListByDealIncludesItsProjects(t *testing.T) {
	e := world(t)
	e.ok("deal", "create", "--party", sierraID, "--holder-role", "advisor")
	e.ok("deal", "create", "--party", elenaID, "--holder-role", "advisor")
	e.ok("project", "create", "--name", "Data room", "--deal", "dl_grupo-sierra")
	e.ok("project", "create", "--name", "Other room", "--deal", "dl_elena-ruiz")
	e.ok(record("On the deal", "--deal", "dl_grupo-sierra")...)
	e.ok(record("On its project", "--project", "pr_data-room")...)
	e.ok(record("On another deal's project", "--project", "pr_other-room")...)
	e.ok(record("On nothing")...)
	out := e.ok("interaction", "list", "--deal", "dl_grupo-sierra")
	var got []string
	for _, it := range out["data"].([]any) {
		got = append(got, it.(map[string]any)["subject"].(string))
	}
	slices.Sort(got)
	if fmt.Sprint(got) != "[On its project On the deal]" {
		t.Errorf("--deal lists %v", got)
	}
	if f := obj(t, out, "filters"); f["deal"] != "dl_grupo-sierra" {
		t.Errorf("filters %v", f)
	}
}

func TestStdinTextDropsOneTrailingNewlineAndAFileDoesNot(t *testing.T) {
	e := world(t)
	for stdin, want := range map[string]string{"From a pipe.\n": "From a pipe.", "Two\n\n": "Two\n", "No newline": "No newline", "Windows\r\n": "Windows"} {
		out := e.runWithStdin(stdin, "interaction", "create", "--channel", "email", "--occurred-at", "2026-11-23T17:02:00Z",
			"--subject", "Stdin "+want, "--participant", elenaID, "--summary-file", "-")
		if out.Code != 0 {
			t.Fatalf("%q: %s", stdin, out.Stderr)
		}
		if got := data(t, out.JSON(t))["summary"]; got != want {
			t.Errorf("stdin %q: summary %q, want %q", stdin, got, want)
		}
	}
	file := filepath.Join(t.TempDir(), "s.txt")
	must(t, os.WriteFile(file, []byte("From a file.\n"), 0o644))
	if got := data(t, e.ok(record("From file", "--summary-file", file)...))["summary"]; got != "From a file.\n" {
		t.Errorf("file summary %q", got)
	}
}

func TestReferencedInteractionHintNamesTheSteps(t *testing.T) {
	e := taskWorld(t)
	e.ok(record("Data room documentation")...)
	e.ok(mkTask("Follow up", "--interaction", dataRoomID)...)
	eo := e.refused("REFERENCED", 4, "interaction", "delete", dataRoomID)
	hint, _ := eo["hint"].(string)
	if !strings.Contains(hint, "delete the task") || !strings.Contains(hint, "then the interaction") || !strings.Contains(hint, "create both again") {
		t.Errorf("hint %q", hint)
	}
}
