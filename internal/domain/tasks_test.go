package domain

import (
	"testing"
	"time"

	"go.jetify.com/typeid"
)

// taskIDAt is a task ID whose TypeID holds the given instant.
func taskIDAt(t *testing.T, at time.Time) string {
	t.Helper()
	ms := uint64(at.UnixMilli())
	b := []byte{byte(ms >> 40), byte(ms >> 32), byte(ms >> 24), byte(ms >> 16), byte(ms >> 8), byte(ms),
		0x70, 0x01, 0x80, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	id, err := typeid.FromUUIDBytesWithPrefix("tk", b)
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func TestTaskMonthIsTheUTCMonthOfTheTypeID(t *testing.T) {
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		// 23:59:59 UTC is already December in Madrid, and still November here.
		{time.Date(2026, 11, 30, 23, 59, 59, 0, time.UTC), "2026-11"},
		{time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), "2026-12"},
		{time.Date(2027, 1, 15, 12, 0, 0, 0, time.UTC), "2027-01"},
	} {
		id := taskIDAt(t, c.at)
		if got := typeIDMonth(id); got != c.want {
			t.Errorf("%s: month %q, want %q", id, got, c.want)
		}
		if want := "records/tasks/" + c.want + "/" + id + ".json"; tasks.Path(id) != want {
			t.Errorf("path %q, want %q", tasks.Path(id), want)
		}
		if !tasks.validID(id) {
			t.Errorf("%s is not a task ID", id)
		}
	}
	for _, bad := range []string{"", "tk_", "tk_zz", "not an id", "tk_01jdx7v3k2f9q8w4e6r5t2y1u0-2"} {
		if tasks.validID(bad) {
			t.Errorf("%q must not be a task ID", bad)
		}
		_ = typeIDMonth(bad) // copes with any string
	}
}
