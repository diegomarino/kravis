package main

import (
	"reflect"
	"testing"
)

// Every list echoes all the filters it applied, normalized, with the live-only
// default as live: true (docs/cli.md, "Output").
func TestWORKSPACE_AC_05_EveryListEchoesTheFiltersItApplied(t *testing.T) {
	e := world(t)
	e.ok(mkDeal("--name", "Zeta")...)
	e.ok("deal", "stage", sierraDeal, "won")
	e.ok(mkProject("--deal", sierraDeal)...)
	for name, tt := range map[string]struct {
		args []string
		want map[string]any
	}{
		"interaction": {[]string{"interaction", "list", "--channel", "email", "--participant", elenaEmail},
			map[string]any{"holder": orgID, "live": true, "channel": "email", "participant": elenaID}},
		"task": {[]string{"task", "list", "--assignee", acmeHuman, "--due-before", "2026-12-31", "--overdue"},
			map[string]any{"holder": orgID, "live": true, "assignee": acmeHuman, "due_before": "2026-12-31", "overdue": true}},
		"deal": {[]string{"deal", "list", "--party", sierraID, "--stage", "won"},
			map[string]any{"holder": orgID, "party": sierraID, "stage": "won"}},
		"project": {[]string{"project", "list", "--deal", sierraDeal},
			map[string]any{"holder": orgID, "live": true, "deal": sierraDeal}},
		"note": {[]string{"note", "list", "--related-to", elenaEmail},
			map[string]any{"holder": orgID, "live": true, "related_to": elenaID}},
		"person":       {[]string{"person", "list"}, map[string]any{"live": true}},
		"organization": {[]string{"organization", "list"}, map[string]any{"live": true}},
		"user":         {[]string{"user", "list"}, map[string]any{"live": true}},
		"all":          {[]string{"person", "list", "--all"}, nil},
	} {
		got, _ := e.ok(tt.args...)["filters"].(map[string]any)
		if !reflect.DeepEqual(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
			t.Errorf("%s: filters %v, want %v", name, got, tt.want)
		}
	}
}
