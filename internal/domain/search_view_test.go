package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestASearchHitNeverCopiesItsSyntheticNamesFromTheRecord(t *testing.T) {
	raw := func(x any) json.RawMessage { b, _ := json.Marshal(x); return b }
	v := View{{Name: "id", Value: raw("pe_x")}, {Name: "type", Value: raw("own")}, {Name: "matches", Value: raw("own")},
		{Name: "updated_at", Value: raw("own")}}
	got := searchView("person", v, time.Unix(0, 0), nil)
	seen := map[string]int{}
	for _, f := range got {
		seen[f.Name]++
	}
	for _, n := range []string{"type", "matches", "updated_at", "id"} {
		if seen[n] != 1 {
			t.Errorf("%s appears %d times in %v", n, seen[n], got)
		}
	}
	if string(got[0].Value) != `"person"` {
		t.Errorf("type = %s", got[0].Value)
	}
}
