package main

import (
	"slices"
	"testing"
)

func TestWORK_AC_02_ATaskListFiltersByRelatedRecord(t *testing.T) {
	e := noteWorld(t) // holder A: deal dl_grupo-sierra, its project pr_data-room
	mk := func(title string, extra ...string) string { return idOf(t, e.ok(mkTask(title, extra...)...)) }
	related := mk("Related to Elena", "--related", elenaID)
	inProject := mk("In the project", "--project", "pr_data-room")
	ofDeal := mk("Of the deal", "--deal", sierraDeal)
	toProject := mk("Related to the project", "--related", "pr_data-room")
	toDeal := mk("Related to the deal", "--related", sierraDeal)
	toOrg := mk("Related to the organization", "--related", sierraID)
	finished := mk("Finished, related to Elena", "--related", elenaID)
	e.ok("task", "status", finished, "done")
	mk("Unrelated")
	theirs := idOf(t, e.ok(as(otherAgent, "task", "create", "--title", "Theirs", "--assignee", otherHuman, "--related", elenaID)...))

	list := func(args ...string) []string {
		out := e.ok(slices.Concat([]string{"task", "list", "--limit", "0"}, args)...)
		got := ids(t, out)
		if out["total"] != float64(len(got)) {
			t.Errorf("%v: total %v for %d tasks", args, out["total"], len(got))
		}
		slices.Sort(got)
		return got
	}
	want := func(ids ...string) []string { slices.Sort(ids); return ids }
	for name, tt := range map[string]struct {
		args []string
		want []string
	}{
		"a person":                 {[]string{"--related-to", elenaID}, want(related)},
		"a person by email":        {[]string{"--related-to", elenaEmail}, want(related)},
		"finished ones with --all": {[]string{"--related-to", elenaID, "--all"}, want(related, finished)},
		"an organization":          {[]string{"--related-to", sierraID}, want(toOrg)},
		"a project: its tasks and the tasks related to it":         {[]string{"--related-to", "pr_data-room"}, want(inProject, toProject)},
		"a deal: its tasks, its projects' tasks and those related": {[]string{"--related-to", sierraDeal}, want(ofDeal, inProject, toDeal)},
		"with another filter": {[]string{"--related-to", sierraDeal, "--project", "pr_data-room"}, want(inProject)},
		"all holders":         {[]string{"--related-to", elenaID, "--all-holders"}, want(related, theirs)},
	} {
		if got := list(tt.args...); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", name, got, tt.want)
		}
	}
	// The other holder's actor sees only its own.
	if got := ids(t, e.ok(as(otherAgent, "task", "list", "--related-to", elenaID)...)); !slices.Equal(got, []string{theirs}) {
		t.Errorf("other holder: %v", got)
	}
	// A cursor belongs to its filters, and a value that is not a related record is refused.
	page := e.ok("task", "list", "--limit", "1", "--related-to", sierraDeal)
	e.refused("INVALID_VALUE", 2, "task", "list", "--limit", "1", "--related-to", elenaID, "--cursor", page["next_cursor"].(string))
	e.refused("INVALID_VALUE", 2, "task", "list", "--related-to", acmeHuman)
	e.refused("NOT_FOUND", 3, "task", "list", "--related-to", "nobody@example.com")
}

func TestWORKSPACE_AC_05_NotesListFilterByRelatedRecord(t *testing.T) {
	e := noteWorld(t)
	id := func(text string, subjects ...string) string { return idOf(t, e.ok(mkNote(t, text, subjects...)...)) }
	elena := id("About Elena", elenaID)
	both := id("About Elena and the deal", elenaEmail, sierraDeal)
	project := id("About the project", "pr_data-room")
	org := id("About the organization", sierraID)
	theirs := idOf(t, e.ok(as(otherAgent, mkNote(t, "Theirs", elenaID)...)...))

	list := func(args ...string) []string {
		out := e.ok(slices.Concat([]string{"note", "list"}, args)...)
		got := ids(t, out)
		if out["total"] != float64(len(got)) {
			t.Errorf("%v: total %v for %d notes", args, out["total"], len(got))
		}
		slices.Sort(got)
		return got
	}
	want := func(ids ...string) []string { slices.Sort(ids); return ids }
	for name, tt := range map[string]struct {
		args []string
		want []string
	}{
		"a person":        {[]string{"--related-to", elenaID}, want(elena, both)},
		"by email":        {[]string{"--related-to", elenaEmail}, want(elena, both)},
		"a deal":          {[]string{"--related-to", sierraDeal}, want(both)},
		"a project":       {[]string{"--related-to", "pr_data-room"}, want(project)},
		"an organization": {[]string{"--related-to", sierraID}, want(org)},
		"all holders":     {[]string{"--related-to", elenaID, "--all-holders"}, want(elena, both, theirs)},
	} {
		if got := list(tt.args...); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", name, got, tt.want)
		}
	}
	page := e.ok("note", "list", "--limit", "1", "--related-to", elenaID)
	e.refused("INVALID_VALUE", 2, "note", "list", "--limit", "1", "--related-to", sierraID, "--cursor", page["next_cursor"].(string))
	e.refused("INVALID_VALUE", 2, "note", "list", "--related-to", acmeHuman)
}

func TestINTERACTIONS_AC_02_AnOutreachMonthIsListedByDirectionInitiatorAndDates(t *testing.T) {
	e := world(t)
	mk := func(actor, at, direction, initiator string) string {
		args := []string{"interaction", "create", "--channel", "email", "--occurred-at", at, "--subject", direction + " " + at,
			"--participant", elenaID, "--direction", direction, "--initiated-by", initiator}
		return idOf(t, e.ok(as(actor, args...)...))
	}
	first := mk(acmeHuman, "2026-11-01T09:00:00+01:00", "outbound", acmeHuman) // the first day, from midnight
	mid := mk(acmeHuman, "2026-11-12T10:00:00Z", "outbound", acmeHuman)
	last := mk(acmeHuman, "2026-11-30T22:30:00Z", "outbound", acmeHuman) // 23:30 in Madrid: the last day
	// Left out: inbound, another initiator (an agent, another human), the previous and the next month.
	mk(acmeHuman, "2026-11-15T10:00:00Z", "inbound", elenaID)
	mk(acmeAgent, "2026-11-16T10:00:00Z", "outbound", acmeAgent)
	mk(otherHuman, "2026-11-17T10:00:00Z", "outbound", otherHuman)
	mk(acmeHuman, "2026-10-31T22:30:00Z", "outbound", acmeHuman) // 23:30 on 31 October in Madrid
	mk(acmeHuman, "2026-11-30T23:30:00Z", "outbound", acmeHuman) // 00:30 on 1 December in Madrid
	mk(acmeHuman, "2026-12-02T10:00:00Z", "outbound", acmeHuman)

	out := e.ok("interaction", "list", "--limit", "0", "--participant", elenaID,
		"--direction", "outbound", "--initiated-by", acmeHuman, "--from", "2026-11-01", "--to", "2026-11-30")
	got := ids(t, out)
	if want := []string{last, mid, first}; !slices.Equal(got, want) || out["total"] != float64(3) {
		t.Errorf("outreach of Ana in November: %v (total %v), want %v", got, out["total"], want)
	}
	// Her agents' outreach counts only when asked for.
	with := e.ok("interaction", "list", "--limit", "0", "--direction", "outbound", "--initiated-by", acmeHuman, "--with-agents",
		"--from", "2026-11-01", "--to", "2026-11-30")
	if with["total"] != float64(4) {
		t.Errorf("with agents: %v", with["total"])
	}
}

// A follow-up made from an email with a person has no --related, and the
// briefing of that person still finds it through the interaction.
func TestWORK_AC_02_ATaskListRelatedToAlsoFindsTheFollowUpsOfItsInteraction(t *testing.T) {
	e := noteWorld(t)
	e.ok(record("Data room documentation")...) // participant: Elena
	followUp := idOf(t, e.ok(mkTask("Send the proposal", "--interaction", dataRoomID)...))
	e.ok(mkTask("Unrelated")...)

	if got := ids(t, e.ok("task", "list", "--related-to", elenaID)); !slices.Equal(got, []string{followUp}) {
		t.Errorf("elena: %v, want the follow-up", got)
	}
	if got := ids(t, e.ok("task", "list", "--related-to", "pe_marco-silva")); len(got) != 0 {
		t.Errorf("marco is no participant of the follow-up's interaction: %v", got)
	}
}
