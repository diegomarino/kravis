package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testPerson struct {
	Base
	Name   string `json:"name"`
	Emails IDs    `json:"emails,omitempty"`
	Phone  string `json:"phone,omitempty"`
}

func personPath(id string) string { return "records/people/" + id + ".json" }

func personKeys(p *testPerson) []Key {
	var keys []Key
	for _, e := range p.Emails {
		keys = append(keys, Key{Kind: "person-email", Value: e})
	}
	return keys
}

func taskPath(id string) string { return "records/tasks/2026-11/" + id + ".json" }

// clock returns a time that moves one minute per write.
func clock() func() time.Time {
	t := at("2026-11-24T09:00:00Z")
	return func() time.Time { t = t.Add(time.Minute); return t }
}

func newTestStore() (*Store, *Memory) {
	m := NewMemory()
	return NewStore(m, clock()), m
}

func text(s string) *string { return &s }

// crash is what the hook panics with to stop a write half way, as a killed
// process would: nothing after it runs, not even a rollback.
type crash struct{}

func crashOn(m *Memory, op Op, path string) {
	m.Hook = func(o Op, p string) error {
		if o == op && p == path {
			panic(crash{})
		}
		return nil
	}
}

func survive(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != (crash{}) {
			t.Fatalf("want a crash, got %v", r)
		}
	}()
	f()
}

func createPerson(t *testing.T, s *Store, id, name string, emails ...string) {
	t.Helper()
	p := &testPerson{Base: Base{ID: id}, Name: name, Emails: emails}
	got, existing, err := Create(s, p, "us_a", personPath, personKeys)
	if err != nil || existing || got != id {
		t.Fatalf("create %s: %q, %v, %v", id, got, existing, err)
	}
}

func loadPerson(t *testing.T, s *Store, id string) *testPerson {
	t.Helper()
	var p testPerson
	if _, err := s.Load(personPath(id), &p); err != nil {
		t.Fatal(err)
	}
	return &p
}

func keyPaths(m *Memory) []string {
	var out []string
	for p := range m.Snapshot() {
		if strings.HasPrefix(p, "keys/") {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

func checkKey(t *testing.T, m *Memory, k Key, owner string) {
	t.Helper()
	got, ok := m.Snapshot()[k.Path()]
	if owner == "" {
		if ok {
			t.Errorf("key %s should not exist, holds %s", k.Value, got)
		}
		return
	}
	want := "{\n  \"key\": \"" + k.Value + "\",\n  \"record\": \"" + owner + "\"\n}\n"
	if got != want {
		t.Errorf("key %s = %q, want %q", k.Value, got, want)
	}
}

func email(e string) Key { return Key{Kind: "person-email", Value: e} }

func TestKeyPath(t *testing.T) {
	sum := sha256.Sum256([]byte("ana@example.com"))
	h := hex.EncodeToString(sum[:])
	if got, want := email("ana@example.com").Path(), "keys/person-email/"+h[:2]+"/"+h+".json"; got != want {
		t.Fatalf("path = %s, want %s", got, want)
	}
}

func TestWORKSPACE_INV_08_EveryWriteBumpsVersionAndStampsTheActor(t *testing.T) {
	s, _ := newTestStore()
	createPerson(t, s, "pe_ana", "Ana")
	p := loadPerson(t, s, "pe_ana")
	if p.Version != 1 || p.CreatedBy != "us_a" || p.UpdatedBy != "us_a" ||
		!p.CreatedAt.Equal(at("2026-11-24T09:01:00Z")) || !p.UpdatedAt.Equal(p.CreatedAt) {
		t.Fatalf("after create: %+v", p.Base)
	}
	for i, actor := range []string{"us_b", "us_c"} {
		got, err := Update(s, personPath("pe_ana"), actor, func(p *testPerson) error {
			p.Phone += "1"
			p.Version, p.CreatedBy, p.CreatedAt = 99, "us_forged", time.Time{} // ignored
			return nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		disk := loadPerson(t, s, "pe_ana")
		if disk.Version != i+2 || disk.UpdatedBy != actor || disk.CreatedBy != "us_a" ||
			!disk.CreatedAt.Equal(at("2026-11-24T09:01:00Z")) || !disk.UpdatedAt.After(disk.CreatedAt) {
			t.Fatalf("after update %d: %+v", i+1, disk.Base)
		}
		if got.Version != disk.Version {
			t.Fatalf("returned version %d, disk %d", got.Version, disk.Version)
		}
	}
}

func TestWORKSPACE_INV_07_ARecordsIDNeverChanges(t *testing.T) {
	s, m := newTestStore()
	createPerson(t, s, "pe_ana", "Ana")
	before := m.Snapshot()
	_, err := Update(s, personPath("pe_ana"), "us_a", func(p *testPerson) error {
		p.ID = "pe_other"
		return nil
	}, nil)
	if !errors.Is(err, ErrIDChanged) {
		t.Fatalf("want ErrIDChanged, got %v", err)
	}
	if !maps.Equal(before, m.Snapshot()) {
		t.Fatal("a refused update wrote something")
	}
}

// otherWriter changes the task's title behind the update's back, as another
// process would: read, change, version + 1, write.
func otherWriter(t *testing.T, m *Memory, p string, title string) {
	data, _, err := m.Get(p)
	if err != nil {
		t.Error(err)
		return
	}
	var rec testTask
	if err := Decode(data, &rec); err != nil {
		t.Error(err)
		return
	}
	rec.Title = title
	rec.Version++
	out, _ := Encode(&rec)
	m.Set(p, out)
}

func newTask(t *testing.T, s *Store) string {
	t.Helper()
	rec := &testTask{Base: Base{ID: "tk_1"}, Holder: "org_petalo", Title: "Send the teaser", Status: "todo", Live: true}
	if _, _, err := Create(s, rec, "us_a", taskPath, nil); err != nil {
		t.Fatal(err)
	}
	return taskPath("tk_1")
}

func TestWORKSPACE_INV_09_AConcurrentChangeToAnotherFieldSurvives(t *testing.T) {
	s, m := newTestStore()
	p := newTask(t, s)
	replaces := 0
	m.Hook = func(op Op, path string) error {
		if op == OpReplace && path == p {
			replaces++
			if replaces == 1 {
				otherWriter(t, m, p, "Send the teaser and the NDA")
			}
		}
		return nil
	}
	got, err := Update(s, p, "us_b", func(r *testTask) error { r.Status = "done"; return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	var disk testTask
	if _, err := s.Load(p, &disk); err != nil {
		t.Fatal(err)
	}
	t.Logf("after a concurrent change: title %q (theirs), status %q (ours), version %d", disk.Title, disk.Status, disk.Version)
	if disk.Title != "Send the teaser and the NDA" || disk.Status != "done" {
		t.Fatalf("both changes must survive, got title %q, status %q", disk.Title, disk.Status)
	}
	if disk.Version != 3 || got.Version != 3 || replaces != 2 {
		t.Fatalf("version %d (returned %d) after %d replaces, want 3 after 2", disk.Version, got.Version, replaces)
	}
}

func TestWORKSPACE_INV_09_ThreeChangesInARowGiveAVersionConflict(t *testing.T) {
	s, m := newTestStore()
	p := newTask(t, s)
	replaces := 0
	m.Hook = func(op Op, path string) error {
		if op == OpReplace && path == p {
			replaces++
			otherWriter(t, m, p, "Title "+string(rune('0'+replaces)))
		}
		return nil
	}
	_, err := Update(s, p, "us_b", func(r *testTask) error {
		r.Status = "done"
		r.Text = text("our description")
		return nil
	}, nil)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("want ErrVersionConflict, got %v", err)
	}
	m.Hook = nil
	var disk testTask
	_, _ = s.Load(p, &disk)
	t.Logf("after three changes in a row: title %q, status %q, version %d, error %q", disk.Title, disk.Status, disk.Version, err)
	if disk.Status != "todo" || disk.Title != "Title 3" || disk.Version != 4 || replaces != Attempts {
		t.Fatalf("got title %q, status %q, version %d after %d replaces", disk.Title, disk.Status, disk.Version, replaces)
	}
	if txt, _ := s.Text(p); txt != "" {
		t.Fatalf("the sidecar of a conflicting update stays as it was, got %q", txt)
	}
}

func TestUpdateOnTheLocalFilesystem(t *testing.T) {
	s := NewStore(NewLocal(t.TempDir()), clock())
	p := newTask(t, s)
	if _, err := Update(s, p, "us_b", func(r *testTask) error { r.Status = "done"; return nil }, nil); err != nil {
		t.Fatal(err)
	}
	var disk testTask
	if _, err := s.Load(p, &disk); err != nil || disk.Status != "done" || disk.Version != 2 {
		t.Fatalf("got %+v, %v", disk, err)
	}
}

func TestUpdateErrors(t *testing.T) {
	s, m := newTestStore()
	if _, err := Update(s, "records/tasks/nope.json", "us_a", func(*testTask) error { return nil }, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing record: %v", err)
	}
	m.Set("records/tasks/bad.json", []byte("not json"))
	if _, err := Update(s, "records/tasks/bad.json", "us_a", func(*testTask) error { return nil }, nil); err == nil {
		t.Error("a broken record must not be updated")
	}
	if _, err := s.Load("records/tasks/bad.json", &testTask{}); err == nil {
		t.Error("a broken record must not load")
	}
	p := newTask(t, s)
	refused := errors.New("refused")
	if _, err := Update(s, p, "us_a", func(*testTask) error { return refused }, nil); !errors.Is(err, refused) {
		t.Errorf("apply's error: %v", err)
	}
	m.Hook = func(op Op, _ string) error {
		if op == OpReplace {
			return ErrUnavailable
		}
		return nil
	}
	if _, err := Update(s, p, "us_a", func(r *testTask) error { r.Status = "done"; return nil }, nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("an outage is not retried: %v", err)
	}
}

func TestUpdateThatChangesNothingWritesNothing(t *testing.T) {
	s, m := newTestStore()
	p := newTask(t, s)
	before := m.Snapshot()
	seen := 0
	got, err := Update(s, p, "us_b", func(r *testTask) error { seen = r.Version; return nil }, nil)
	if err != nil || got.Version != seen || got.UpdatedBy == "us_b" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if !maps.Equal(before, m.Snapshot()) {
		t.Error("an update that changes nothing wrote")
	}
	// A new sidecar text is a change.
	got, err = Update(s, p, "us_b", func(r *testTask) error { r.Text = text("new"); return nil }, nil)
	if err != nil || got.Version != seen+1 {
		t.Fatalf("text change: %+v, %v", got, err)
	}
}

func TestFieldsAndNames(t *testing.T) {
	p := &testPerson{Base: Base{ID: "pe_ana", Version: 2, Unknown: map[string]json.RawMessage{"zz": []byte(`1`)}}, Name: "Ana"}
	fs, err := Fields(p)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range fs {
		names = append(names, f.Name)
	}
	if !slices.Equal(names, []string{"id", "name", "zz", "version"}) {
		t.Errorf("Fields = %v", names)
	}
	if got := FieldNames(p); !slices.Equal(got, []string{"id", "name", "emails", "phone", "version", "created_at", "created_by", "updated_at", "updated_by"}) {
		t.Errorf("FieldNames = %v", got)
	}
}

func TestWORKSPACE_INV_10_CreatingAnExistingNaturalKeyReturnsTheExistingRecord(t *testing.T) {
	s, m := newTestStore()
	createPerson(t, s, "pe_ana", "Ana", "ana@example.com")
	before := m.Snapshot()

	again := &testPerson{Base: Base{ID: "pe_ana-2"}, Name: "Ana M.", Emails: IDs{"ana@example.com"}}
	id, existing, err := Create(s, again, "us_b", personPath, personKeys)
	if err != nil || !existing || id != "pe_ana" {
		t.Fatalf("got %q, existing %v, %v", id, existing, err)
	}
	if !maps.Equal(before, m.Snapshot()) {
		t.Fatal("returning the existing record wrote something")
	}
}

func TestCreateCompletesARecordThatCrashedAfterItsKey(t *testing.T) {
	s, m := newTestStore()
	first := &testPerson{Base: Base{ID: "pe_ana"}, Name: "Ana", Emails: IDs{"ana@example.com"}}
	crashOn(m, OpCreate, personPath("pe_ana"))
	survive(t, func() { _, _, _ = Create(s, first, "us_a", personPath, personKeys) })
	m.Hook = nil
	checkKey(t, m, email("ana@example.com"), "pe_ana") // step 2 done, step 3 not

	retry := &testPerson{Base: Base{ID: "pe_ana-2"}, Name: "Ana", Emails: IDs{"ana@example.com"}}
	id, existing, err := Create(s, retry, "us_a", personPath, personKeys)
	if err != nil || existing || id != "pe_ana" {
		t.Fatalf("got %q, existing %v, %v", id, existing, err)
	}
	if got := loadPerson(t, s, "pe_ana"); got.Name != "Ana" || got.Version != 1 {
		t.Fatalf("record = %+v", got)
	}
	if _, ok := m.Snapshot()[personPath("pe_ana-2")]; ok {
		t.Fatal("the retry created a duplicate")
	}
}

func TestCreateWithSeveralKeysCompletedByARetry(t *testing.T) {
	s, m := newTestStore()
	keys := IDs{"ana@example.com", "ana.mora@example.org"}
	first := &testPerson{Base: Base{ID: "pe_ana"}, Name: "Ana", Emails: keys}
	crashOn(m, OpCreate, email("ana.mora@example.org").Path()) // after the first key
	survive(t, func() { _, _, _ = Create(s, first, "us_a", personPath, personKeys) })
	m.Hook = nil

	retry := &testPerson{Base: Base{ID: "pe_ana-2"}, Name: "Ana", Emails: keys}
	id, existing, err := Create(s, retry, "us_a", personPath, personKeys)
	if err != nil || existing || id != "pe_ana" {
		t.Fatalf("got %q, existing %v, %v", id, existing, err)
	}
	for _, e := range keys {
		checkKey(t, m, email(e), "pe_ana")
	}
	if n := len(m.Snapshot()); n != 3 {
		t.Fatalf("want 2 keys and 1 record, got %v", slices.Sorted(maps.Keys(m.Snapshot())))
	}
}

func TestCreateWithSeveralKeysWhereALaterOneBelongsToAnotherRecord(t *testing.T) {
	s, m := newTestStore()
	createPerson(t, s, "pe_ben", "Ben", "ben@example.com")
	before := m.Snapshot()

	cy := &testPerson{Base: Base{ID: "pe_cy"}, Name: "Cy", Emails: IDs{"cy@example.com", "cy2@example.com", "ben@example.com"}}
	id, existing, err := Create(s, cy, "us_a", personPath, personKeys)
	if err != nil || !existing || id != "pe_ben" {
		t.Fatalf("got %q, existing %v, %v", id, existing, err)
	}
	if !maps.Equal(before, m.Snapshot()) {
		t.Fatalf("something was left behind: %v", keyPaths(m))
	}

	// The same, when Ben's key appears while Cy's keys are being created:
	// the keys Cy already created are deleted again.
	s2, m2 := newTestStore()
	createPerson(t, s2, "pe_ben", "Ben")
	ben := email("ben@example.com")
	benKey, _ := writeObject([]field{{"key", mustMarshal(ben.Value)}, {"record", mustMarshal("pe_ben")}})
	m2.Hook = func(op Op, p string) error {
		if op == OpCreate && p == ben.Path() {
			m2.Hook = nil
			m2.Set(p, benKey)
		}
		return nil
	}
	before = m2.Snapshot()
	before[ben.Path()] = string(benKey)
	cy.ID, cy.Text = "pe_cy", text("Met at the fair")
	id, existing, err = Create(s2, cy, "us_a", personPath, personKeys)
	if err != nil || !existing || id != "pe_ben" {
		t.Fatalf("racing: got %q, existing %v, %v", id, existing, err)
	}
	if !maps.Equal(before, m2.Snapshot()) {
		t.Fatalf("racing: something was left behind: %v", slices.Sorted(maps.Keys(m2.Snapshot())))
	}
}

func TestCreateErrors(t *testing.T) {
	s, m := newTestStore()
	createPerson(t, s, "pe_ana", "Ana")
	again := &testPerson{Base: Base{ID: "pe_ana"}, Name: "Ana"}
	if _, _, err := Create(s, again, "us_a", personPath, nil); !errors.Is(err, ErrExists) {
		t.Errorf("a taken path: %v", err)
	}
	// Two keys left by two crashed attempts with different IDs.
	m.Set(email("x@example.com").Path(), []byte(`{"key":"x@example.com","record":"pe_x"}`))
	m.Set(email("y@example.com").Path(), []byte(`{"key":"y@example.com","record":"pe_y"}`))
	xy := &testPerson{Base: Base{ID: "pe_z"}, Name: "Z", Emails: IDs{"x@example.com", "y@example.com"}}
	if _, _, err := Create(s, xy, "us_a", personPath, personKeys); !errors.Is(err, ErrKeyConflict) {
		t.Errorf("split keys: %v", err)
	}
	m.Set(email("bad@example.com").Path(), []byte(`not json`))
	bad := &testPerson{Base: Base{ID: "pe_bad"}, Name: "Bad", Emails: IDs{"bad@example.com"}}
	if _, _, err := Create(s, bad, "us_a", personPath, personKeys); err == nil {
		t.Error("a broken key file must stop the create")
	}
	// An outage while writing the record rolls back its keys and sidecar.
	before := m.Snapshot()
	m.Hook = func(op Op, p string) error {
		if op == OpCreate && p == personPath("pe_new") {
			return ErrUnavailable
		}
		return nil
	}
	nw := &testPerson{Base: Base{ID: "pe_new", Text: text("notes")}, Name: "New", Emails: IDs{"new@example.com"}}
	if _, _, err := Create(s, nw, "us_a", personPath, personKeys); !errors.Is(err, ErrUnavailable) {
		t.Errorf("outage: %v", err)
	}
	if !maps.Equal(before, m.Snapshot()) {
		t.Errorf("an outage left %v", slices.Sorted(maps.Keys(m.Snapshot())))
	}
}

func TestUpdateReconcilesKeys(t *testing.T) {
	s, m := newTestStore()
	createPerson(t, s, "pe_ana", "Ana", "ana@example.com")
	createPerson(t, s, "pe_ben", "Ben", "ben@example.com")
	p := personPath("pe_ana")

	// Several added keys: the first ones free, a later one Ben's. Nothing
	// is written and the keys created on the way are deleted again.
	before := m.Snapshot()
	_, err := Update(s, p, "us_a", func(r *testPerson) error {
		r.Emails = append(r.Emails, "ana2@example.com", "ana3@example.com", "ben@example.com")
		r.Text = text("should not be written")
		return nil
	}, personKeys)
	if !errors.Is(err, ErrKeyConflict) {
		t.Fatalf("want ErrKeyConflict, got %v", err)
	}
	if !maps.Equal(before, m.Snapshot()) {
		t.Fatalf("a refused update left %v", slices.Sorted(maps.Keys(m.Snapshot())))
	}

	// Replace one email by another: the new key first, then the record,
	// then the old key is deleted.
	var ops []string
	m.Hook = func(op Op, path string) error {
		if op != OpGet {
			ops = append(ops, string(op)+" "+path)
		}
		return nil
	}
	got, err := Update(s, p, "us_a", func(r *testPerson) error { r.Emails = IDs{"ana.mora@example.org"}; return nil }, personKeys)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"create " + email("ana.mora@example.org").Path(),
		"replace " + p,
		"delete " + email("ana@example.com").Path(),
	}
	if !slices.Equal(ops, want) {
		t.Fatalf("ops = %v, want %v", ops, want)
	}
	checkKey(t, m, email("ana.mora@example.org"), "pe_ana")
	checkKey(t, m, email("ana@example.com"), "")
	checkKey(t, m, email("ben@example.com"), "pe_ben")
	if got.Version != 2 {
		t.Fatalf("version %d", got.Version)
	}

	// A key left by an interrupted update that already points to the record
	// is kept as it is.
	m.Hook = nil
	left := email("ana.alt@example.net")
	leftKey, _ := writeObject([]field{{"key", mustMarshal(left.Value)}, {"record", mustMarshal("pe_ana")}})
	m.Set(left.Path(), leftKey)
	if _, err := Update(s, p, "us_a", func(r *testPerson) error { r.Emails = append(r.Emails, left.Value); return nil }, personKeys); err != nil {
		t.Fatal(err)
	}
	checkKey(t, m, left, "pe_ana")
}

func TestUpdateRetryDropsTheKeysOfTheLostAttempt(t *testing.T) {
	s, m := newTestStore()
	createPerson(t, s, "pe_ana", "Ana")
	p := personPath("pe_ana")
	replaces := 0
	m.Hook = func(op Op, path string) error {
		if op == OpReplace && path == p {
			if replaces++; replaces == 1 {
				data, _, _ := m.Get(p)
				m.Set(p, data) // a concurrent write, same content
			}
		}
		return nil
	}
	attempt := 0
	_, err := Update(s, p, "us_a", func(r *testPerson) error {
		attempt++
		r.Emails = IDs{"try" + string(rune('0'+attempt)) + "@example.com"}
		return nil
	}, personKeys)
	if err != nil {
		t.Fatal(err)
	}
	checkKey(t, m, email("try1@example.com"), "")
	checkKey(t, m, email("try2@example.com"), "pe_ana")
}

func TestSidecars(t *testing.T) {
	s, m := newTestStore()
	var ops []string
	m.Hook = func(op Op, path string) error {
		if op != OpGet {
			ops = append(ops, string(op)+" "+path)
		}
		return nil
	}
	rec := &testTask{Base: Base{ID: "tk_1", Text: text("# Teaser\n\nSend it.\n")}, Holder: "org_petalo", Title: "Send", Live: true}
	p := taskPath("tk_1")
	if _, _, err := Create(s, rec, "us_a", taskPath, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"create " + SidecarPath(p), "create " + p}; !slices.Equal(ops, want) {
		t.Fatalf("ops = %v, want the .md first: %v", ops, want)
	}
	if got, _ := s.Text(p); got != "# Teaser\n\nSend it.\n" {
		t.Fatalf("text = %q", got)
	}

	steps := []struct {
		text *string
		want string
	}{
		{nil, "# Teaser\n\nSend it.\n"}, // nil leaves it alone
		{text("Sent."), "Sent."},
		{text("Sent."), "Sent."}, // unchanged
		{text(""), ""},           // removed
		{text(""), ""},           // still none
	}
	for _, st := range steps {
		if _, err := Update(s, p, "us_a", func(r *testTask) error { r.Text = st.text; return nil }, nil); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Text(p); got != st.want {
			t.Fatalf("text = %q, want %q", got, st.want)
		}
	}
	if _, ok := m.Snapshot()[SidecarPath(p)]; ok {
		t.Fatal("an empty text leaves no sidecar")
	}

	// A sidecar left by a crash before its JSON is overwritten by the retry.
	m.Set(SidecarPath(taskPath("tk_2")), []byte("stale"))
	rec2 := &testTask{Base: Base{ID: "tk_2", Text: text("fresh")}, Holder: "org_petalo", Title: "B", Live: true}
	if _, _, err := Create(s, rec2, "us_a", taskPath, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Text(taskPath("tk_2")); got != "fresh" {
		t.Fatalf("text = %q", got)
	}
	m.Hook = func(Op, string) error { return ErrUnavailable }
	if _, err := s.Text(p); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an outage is not an empty text: %v", err)
	}
}

func TestSidecarUndoKeepsSomeoneElsesText(t *testing.T) {
	s, m := newTestStore()
	p := newTask(t, s)
	m.Hook = func(op Op, path string) error {
		if op == OpReplace && path == p {
			otherWriter(t, m, p, "theirs")
			m.Set(SidecarPath(p), []byte("their text"))
		}
		return nil
	}
	_, err := Update(s, p, "us_b", func(r *testTask) error { r.Text = text("our text"); return nil }, nil)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("got %v", err)
	}
	m.Hook = nil
	if got, _ := s.Text(p); got != "their text" {
		t.Fatalf("text = %q", got)
	}
}

func TestWORKSPACE_INV_11_DeleteIsConditionalAndRemovesSidecarAndKeys(t *testing.T) {
	s, m := newTestStore()
	ana := &testPerson{Base: Base{ID: "pe_ana", Text: text("notes")}, Name: "Ana", Emails: IDs{"ana@example.com", "shared@example.com"}}
	if _, _, err := Create(s, ana, "us_a", personPath, personKeys); err != nil {
		t.Fatal(err)
	}
	p := personPath("pe_ana")

	// The caller reads the record and its token, checks that nothing refers
	// to it (from List and Load), then deletes with that token.
	var rec testPerson
	token, err := s.Load(p, &rec)
	if err != nil {
		t.Fatal(err)
	}
	if paths, _ := s.List("records/"); !slices.Equal(paths, []string{p, SidecarPath(p)}) {
		t.Fatalf("list = %v", paths)
	}

	// The record changed after the check: nothing is removed.
	if _, err := Update(s, p, "us_b", func(r *testPerson) error { r.Phone = "1"; return nil }, nil); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()
	if err := s.Remove(p, rec.ID, token, personKeys(&rec)); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("a stale token: %v", err)
	}
	if !maps.Equal(before, m.Snapshot()) {
		t.Fatal("a refused delete removed something")
	}

	// A key now held by another record is left alone.
	shared := email("shared@example.com")
	other, _ := writeObject([]field{{"key", mustMarshal(shared.Value)}, {"record", mustMarshal("pe_ben")}})
	m.Set(shared.Path(), other)

	token, _ = s.Load(p, &rec)
	var ops []string
	m.Hook = func(op Op, path string) error {
		if op == OpDelete {
			ops = append(ops, path)
		}
		return nil
	}
	if err := s.Remove(p, rec.ID, token, personKeys(&rec)); err != nil {
		t.Fatal(err)
	}
	if want := []string{p, SidecarPath(p), email("ana@example.com").Path()}; !slices.Equal(ops, want) {
		t.Fatalf("deletes = %v, want JSON, sidecar, keys: %v", ops, want)
	}
	if got := slices.Sorted(maps.Keys(m.Snapshot())); !slices.Equal(got, []string{shared.Path()}) {
		t.Fatalf("left behind: %v", got)
	}
	if err := s.Remove(p, rec.ID, token, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a second delete: %v", err)
	}
}

// once runs f from the hook the first time op reaches path, outside any
// nested call that f itself makes.
func once(m *Memory, op Op, path string, f func()) {
	done := false
	m.Hook = func(o Op, p string) error {
		if o == op && p == path && !done {
			done = true
			f()
		}
		return nil
	}
}

func TestUpdateKeepsAKeyThatAConcurrentUpdateCommitted(t *testing.T) {
	s, m := newTestStore()
	createPerson(t, s, "pe_ana", "Ana")
	p := personPath("pe_ana")
	addAlt := func(r *testPerson) error { r.Emails = IDs{"ana.alt@example.com"}; return nil }
	// A created the key; before A's record is written, B adds the same
	// email, reuses A's key and commits.
	once(m, OpReplace, p, func() {
		if _, err := Update(s, p, "us_b", addAlt, personKeys); err != nil {
			t.Error(err)
		}
	})
	if _, err := Update(s, p, "us_a", addAlt, personKeys); err != nil {
		t.Fatal(err)
	}
	checkKey(t, m, email("ana.alt@example.com"), "pe_ana")
}

func TestSidecarUndoKeepsAConcurrentWriteOfTheSameText(t *testing.T) {
	s, m := newTestStore()
	p := newTask(t, s)
	if _, err := Update(s, p, "us_a", func(r *testTask) error { r.Text = text("O"); return nil }, nil); err != nil {
		t.Fatal(err)
	}
	// Each time A is about to write its record, B writes the same text and
	// commits; A gives up after three attempts.
	inB := false
	m.Hook = func(op Op, path string) error {
		if op == OpReplace && path == p && !inB {
			inB = true
			defer func() { inB = false }()
			if _, err := Update(s, p, "us_b", func(r *testTask) error { r.Text = text("X"); return nil }, nil); err != nil {
				t.Error(err)
			}
		}
		return nil
	}
	_, err := Update(s, p, "us_a", func(r *testTask) error { r.Text = text("X"); return nil }, nil)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("got %v", err)
	}
	m.Hook = nil
	if got, _ := s.Text(p); got != "X" {
		t.Fatalf("B's committed text was undone: %q", got)
	}
}

func TestALosingCreateKeepsTheKeysOfTheWinner(t *testing.T) {
	s, m := newTestStore()
	a := &testPerson{Base: Base{ID: "pe_ana", Text: text("A's notes")}, Name: "Ana", Emails: IDs{"ana@example.com"}}
	// A created the key; before A writes its record, B creates the same
	// person, takes the ID from A's key and writes the record first.
	once(m, OpCreate, personPath("pe_ana"), func() {
		b := &testPerson{Base: Base{ID: "pe_ana-2", Text: text("B's notes")}, Name: "Ana", Emails: IDs{"ana@example.com"}}
		if id, existing, err := Create(s, b, "us_b", personPath, personKeys); err != nil || existing || id != "pe_ana" {
			t.Errorf("B: %q, %v, %v", id, existing, err)
		}
	})
	id, existing, err := Create(s, a, "us_a", personPath, personKeys)
	if err != nil || !existing || id != "pe_ana" {
		t.Fatalf("A: %q, %v, %v", id, existing, err)
	}
	checkKey(t, m, email("ana@example.com"), "pe_ana")
	if got := loadPerson(t, s, "pe_ana"); got.CreatedBy != "us_b" {
		t.Fatalf("record = %+v", got.Base)
	}
	if got, _ := s.Text(personPath("pe_ana")); got != "B's notes" {
		t.Fatalf("text = %q", got)
	}
}

func TestCreateCollisionsThatAreNotTheSamePerson(t *testing.T) {
	ana := func() *testPerson {
		return &testPerson{Base: Base{ID: "pe_ana", Text: text("notes")}, Name: "Ana", Emails: IDs{"ana@example.com"}}
	}
	// Another writer creates the sidecar while A writes its own: no record
	// exists, so this is no existing record.
	s, m := newTestStore()
	once(m, OpCreate, SidecarPath(personPath("pe_ana")), func() { m.Set(SidecarPath(personPath("pe_ana")), []byte("theirs")) })
	if id, existing, err := Create(s, ana(), "us_a", personPath, personKeys); !errors.Is(err, ErrExists) || existing {
		t.Fatalf("sidecar collision: %q, %v, %v", id, existing, err)
	}
	checkKey(t, m, email("ana@example.com"), "")

	// Another Ana without that email takes the path first: her record does
	// not hold A's key, so A fails and its key goes.
	s, m = newTestStore()
	once(m, OpCreate, personPath("pe_ana"), func() {
		other, _ := Encode(&testPerson{Base: Base{ID: "pe_ana", Version: 1}, Name: "Ana"})
		m.Set(personPath("pe_ana"), other)
	})
	if id, existing, err := Create(s, ana(), "us_a", personPath, personKeys); !errors.Is(err, ErrExists) || existing {
		t.Fatalf("record collision: %q, %v, %v", id, existing, err)
	}
	checkKey(t, m, email("ana@example.com"), "")
	if _, ok := m.Snapshot()[SidecarPath(personPath("pe_ana"))]; ok {
		t.Fatal("A's sidecar was left behind")
	}
}

func TestUpdateReleasesItsKeysWhenTheRecordIsDeletedMeanwhile(t *testing.T) {
	s, m := newTestStore()
	createPerson(t, s, "pe_ana", "Ana")
	p := personPath("pe_ana")
	once(m, OpReplace, p, func() {
		_, token, _ := m.Get(p)
		_ = m.Delete(p, token)
	})
	_, err := Update(s, p, "us_a", func(r *testPerson) error { r.Emails = IDs{"ana@example.com"}; return nil }, personKeys)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	checkKey(t, m, email("ana@example.com"), "")
}

// An update whose sidecar write lands after a delete read the sidecar's
// token, and whose JSON write finds the record deleted, removes the sidecar
// it wrote instead of restoring the deleted record's: no orphan is left.
func TestAnUpdateThatLosesToADeleteLeavesNoSidecar(t *testing.T) {
	s, m := newTestStore()
	ana := &testPerson{Base: Base{ID: "pe_ana", Text: text("old")}, Name: "Ana"}
	if _, _, err := Create(s, ana, "us_a", personPath, nil); err != nil {
		t.Fatal(err)
	}
	p := personPath("pe_ana")
	var rec testPerson
	token, err := s.Load(p, &rec)
	if err != nil {
		t.Fatal(err)
	}

	atReplace, jsonGone, updated := make(chan struct{}), make(chan struct{}), make(chan error)
	started, cleaned := false, atomic.Bool{}
	m.Hook = func(op Op, path string) error {
		switch {
		case op == OpDelete && path == p && !started: // the delete has read the sidecar's token
			started = true
			go func() {
				_, err := Update(s, p, "us_b", func(r *testPerson) error { r.Text = text("new"); return nil }, nil)
				updated <- err
			}()
			<-atReplace
		case op == OpReplace && path == p: // the update wrote its sidecar
			atReplace <- struct{}{}
			<-jsonGone
		case op == OpDelete && path == SidecarPath(p):
			if cleaned.CompareAndSwap(false, true) { // the delete's cleanup, not the update's own
				close(jsonGone)
				if err := <-updated; !errors.Is(err, ErrNotFound) {
					t.Errorf("the update: %v", err)
				}
			}
		}
		return nil
	}
	if err := s.Remove(p, rec.ID, token, nil); err != nil {
		t.Fatal(err)
	}
	m.Hook = nil
	if left := m.Snapshot(); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}
