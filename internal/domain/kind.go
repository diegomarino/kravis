package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/diegomarino/kravis/internal/storage"
)

// Type describes a record type once: its ID form, its folder, its compact
// view and its default list order. Lists and views use it, and so will
// kravis schema.
type Type struct {
	// Name is the type's name in commands and messages: "person".
	Name string
	// Dir is its folder under records/: "people".
	Dir string
	// Prefix starts every ID of the type: "pe_".
	Prefix string
	// Compact lists the fields of the compact view, in order.
	Compact []string
	// Order says how lists are sorted by default; the ID breaks ties.
	Order string
	// group, when set, gives the folder inside Dir that an ID belongs to,
	// computed from the ID alone ("2026-11"); it must cope with any string.
	group func(id string) string
}

// Path is where the record with the given ID lives.
func (t Type) Path(id string) string {
	if t.group != nil {
		return "records/" + t.Dir + "/" + t.group(id) + "/" + id + ".json"
	}
	return "records/" + t.Dir + "/" + id + ".json"
}

// compactField is a field of a compact view and the JSON it shows when the
// record leaves it empty ("" leaves it out).
type compactField struct {
	name string
	zero string
}

// kind is a record type with the code every type shares: views, lists,
// creates with slug IDs, updates and deletes.
type kind[T any, P interface {
	*T
	storage.Record
}] struct {
	Type
	compact []compactField
	// sortKey is the record's default sort value; the ID breaks ties.
	sortKey func(P) string
	// holder, when set, says whose a work record is: reads and writes made
	// for a holder find only that holder's records (another's is NOT_FOUND).
	holder func(P) string
	// desc sorts by sortKey from the largest, newest first.
	desc bool
	// form is what an ID looks like after its prefix; nil means a slug.
	form *regexp.Regexp
	live func(P) bool
	// keys are the record's natural keys (nil: none), of the kinds in
	// keyKinds.
	keys     func(P) []storage.Key
	keyKinds []string
	// rules reports what is wrong with a record's fields, for writes and
	// check; problems adds its ID.
	rules func(P) []string
	// extraFields are names --fields accepts besides the record's own.
	extraFields []string
}

// problems reports what is wrong with a record: its ID, then its fields.
func (k *kind[T, P]) problems(rec P) []string {
	var out []string
	if id := rec.Meta().ID; !k.validID(id) {
		out = append(out, fmt.Sprintf("%q is not a %s ID (%s<slug>)", id, k.Name, k.Prefix))
	}
	return append(out, k.rules(rec)...)
}

// metaProblems reports what is wrong with a stored record's metadata: a
// version below 1, or no creation time or author. Check reports them and an
// update refuses the record until it is repaired: Kravis never invents who
// created a record, or when.
func metaProblems(b *storage.Base) []string {
	var out []string
	if b.Version < 1 {
		out = append(out, fmt.Sprintf("the version is %d, not a positive number", b.Version))
	}
	if b.CreatedAt.IsZero() {
		out = append(out, "the record has no created_at")
	}
	if b.CreatedBy == "" {
		out = append(out, "the record has no created_by")
	}
	return out
}

func newKind[T any, P interface {
	*T
	storage.Record
}](k kind[T, P]) *kind[T, P] {
	for _, f := range k.compact {
		k.Compact = append(k.Compact, f.name)
	}
	if k.keys == nil {
		k.keys = func(P) []storage.Key { return nil }
	}
	if k.form == nil {
		k.form = slugID
	}
	return &k
}

// slugID is the form of every slug ID after its prefix.
var slugID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (k *kind[T, P]) validID(id string) bool {
	rest, ok := strings.CutPrefix(id, k.Prefix)
	return ok && k.form.MatchString(rest)
}

func (k *kind[T, P]) notFound(ref string) *Error {
	return newError(CodeNotFound, fmt.Sprintf("kravis %s list shows what exists", k.Name),
		"%s %q does not exist", k.Name, ref).with("id", ref)
}

// load reads the record with the given ID and its token.
func (k *kind[T, P]) load(ws *Workspace, id string) (P, storage.Token, error) {
	if !k.validID(id) {
		return nil, "", k.notFound(id)
	}
	rec := P(new(T))
	token, err := ws.Store.Load(k.Path(id), rec)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, "", k.notFound(id)
	}
	return rec, token, err
}

// loadIn is load for a holder: another holder's record does not exist for
// it. An empty holder, or a type without holders, limits nothing.
func (k *kind[T, P]) loadIn(ws *Workspace, holder, id string) (P, storage.Token, error) {
	rec, token, err := k.load(ws, id)
	if err == nil && holder != "" && k.holder != nil && k.holder(rec) != holder {
		return nil, "", k.notFound(id)
	}
	return rec, token, err
}

// applied is the filters a list echoes: each name with its value, leaving out
// the ones not given (an empty string, false or nil).
func applied(pairs ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		switch v := pairs[i+1].(type) {
		case nil:
			continue
		case string:
			if v == "" {
				continue
			}
		case bool:
			if !v {
				continue
			}
		}
		out[pairs[i].(string)] = pairs[i+1]
	}
	return out
}

// holderFilter is what a read limited to a holder says about itself.
func holderFilter(holder string) map[string]any {
	if holder == "" {
		return nil
	}
	return map[string]any{"holder": holder}
}

// View is a record's view for output: its fields in order.
type View []storage.Field

// MarshalJSON writes the view as one object, keeping the field order.
func (v View) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range v {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, _ := json.Marshal(f.Name)
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(f.Value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// ViewOptions widen a view: Fields adds the named fields, Full shows the
// whole record.
type ViewOptions struct {
	Fields []string
	Full   bool
}

// checkFields refuses a --fields name that the type does not define.
func (k *kind[T, P]) checkFields(opt ViewOptions) error {
	allowed := slices.Concat(storage.FieldNames(P(new(T))), k.extraFields)
	for _, f := range opt.Fields {
		if !slices.Contains(allowed, f) {
			return newError(CodeUnknownField, "", "%s has no field %q", k.Name, f).
				with("field", f).with("allowed", allowed)
		}
	}
	return nil
}

// view is the compact view of rec, plus the fields touched and those asked
// for; with Full, the whole record as stored.
func (k *kind[T, P]) view(rec P, touched []string, opt ViewOptions) (View, error) {
	fields, err := storage.Fields(rec)
	if err != nil {
		return nil, err
	}
	if opt.Full {
		return View(fields), nil
	}
	var out View
	for _, c := range k.compact {
		if i := slices.IndexFunc(fields, func(f storage.Field) bool { return f.Name == c.name }); i >= 0 {
			out = append(out, fields[i])
		} else if c.zero != "" {
			out = append(out, storage.Field{Name: c.name, Value: json.RawMessage(c.zero)})
		}
	}
	extra := slices.Concat(touched, opt.Fields)
	for _, f := range fields {
		if slices.Contains(extra, f.Name) && !slices.Contains(k.Compact, f.Name) {
			out = append(out, f)
		}
	}
	return out, nil
}

// Warning is a structured warning of a successful write.
type Warning struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Warning codes.
const (
	WarnSimilarExists  = "SIMILAR_EXISTS"
	WarnGenericAddress = "GENERIC_ADDRESS"
)

// Result is what a write returns: its outcome, the view and any warnings.
type Result struct {
	Outcome  string
	Data     View
	Warnings []Warning
}

// More outcomes of a write (see init.go for the first ones).
const (
	OutcomeUnchanged = "unchanged"
	OutcomeDeleted   = "deleted"
)

// result builds a write's result.
func (k *kind[T, P]) result(outcome string, rec P, touched []string, opt ViewOptions, warnings ...Warning) (*Result, error) {
	v, err := k.view(rec, touched, opt)
	if err != nil {
		return nil, err
	}
	return &Result{Outcome: outcome, Data: v, Warnings: warnings}, nil
}

// get returns the view of the record with the given ID.
func (k *kind[T, P]) get(ws *Workspace, id string, opt ViewOptions) (View, error) {
	return k.getIn(ws, "", id, opt)
}

// getIn is get for a holder: another holder's record is NOT_FOUND.
func (k *kind[T, P]) getIn(ws *Workspace, holder, id string, opt ViewOptions) (View, error) {
	if err := k.checkFields(opt); err != nil {
		return nil, err
	}
	rec, _, err := k.loadIn(ws, holder, id)
	if err != nil {
		return nil, err
	}
	return k.view(rec, nil, opt)
}

// create writes rec under the first free ID of prefix+base, base-2,
// base-3… On a natural key that already exists it writes nothing and
// returns that record as existing. similar names the record whose ID the
// new one had to avoid.
func (k *kind[T, P]) create(ws *Workspace, actor string, rec P, base string) (existing P, similar string, err error) {
	if problems := k.rules(rec); len(problems) > 0 {
		return nil, "", fmt.Errorf("%s: %s", k.Name, problems[0]) // inputs are validated before this
	}
	for range 10 {
		id, sim, err := k.freeID(ws, base)
		if err != nil {
			return nil, "", err
		}
		rec.Meta().ID = id
		got, found, err := storage.Create(ws.Store, rec, actor, k.Path, k.keys)
		if errors.Is(err, storage.ErrExists) {
			continue // another create took the ID: take the next one
		}
		if err != nil {
			return nil, "", err
		}
		if found {
			ex := P(new(T))
			if _, err := ws.Store.Load(k.Path(got), ex); err != nil {
				return nil, "", err
			}
			return ex, "", nil
		}
		if got != id { // completed an earlier, interrupted create
			sim = ""
		}
		return nil, sim, nil
	}
	return nil, "", fmt.Errorf("create %s: %w", k.Name, storage.ErrVersionConflict)
}

// freeID returns the first ID of prefix+base, base-2… that no record has,
// and the existing base ID when it had to add a suffix.
func (k *kind[T, P]) freeID(ws *Workspace, base string) (id, similar string, err error) {
	first := k.Prefix + base
	paths, err := ws.Store.List(k.Path(first)[:len(k.Path(first))-len(".json")])
	if err != nil {
		return "", "", err
	}
	for n := 1; ; n++ {
		id = first
		if n > 1 {
			id, similar = first+"-"+strconv.Itoa(n), first
		}
		if !slices.Contains(paths, k.Path(id)) {
			return id, similar, nil
		}
	}
}

// update applies change to the record with the given ID and reports
// whether anything changed; a change that leaves the record as it was
// writes nothing. change runs again on each attempt.
func (k *kind[T, P]) update(ws *Workspace, actor, id string, change func(P) error) (P, bool, error) {
	return k.updateIn(ws, "", actor, id, change)
}

// updateIn is update for a holder: another holder's record is NOT_FOUND.
func (k *kind[T, P]) updateIn(ws *Workspace, holder, actor, id string, change func(P) error) (P, bool, error) {
	if !k.validID(id) {
		return nil, false, k.notFound(id)
	}
	seen := 0
	rec, err := storage.Update(ws.Store, k.Path(id), actor, func(p P) error {
		if holder != "" && k.holder != nil && k.holder(p) != holder {
			return k.notFound(id)
		}
		seen = p.Meta().Version
		if problems := metaProblems(p.Meta()); len(problems) > 0 {
			return newError(CodeInvalidValue, "repair "+k.Path(id)+" by hand; kravis check lists what is wrong",
				"the %s on disk is invalid: %s", k.Name, problems[0])
		}
		if err := change(p); err != nil {
			return err
		}
		if problems := k.problems(p); len(problems) > 0 {
			return newError(CodeInvalidValue, "", "the %s would be invalid: %s", k.Name, problems[0])
		}
		return nil
	}, k.keys)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false, k.notFound(id)
	}
	if err != nil {
		return nil, false, err
	}
	return rec, rec.Meta().Version != seen, nil
}

// remove deletes the record with the given ID, unless another record
// refers to it (REFERENCED), and returns the view of what it was.
func (k *kind[T, P]) remove(ws *Workspace, id string, opt ViewOptions) (*Result, error) {
	return k.removeIn(ws, "", id, opt)
}

// removeIn is remove for a holder: another holder's record is NOT_FOUND.
func (k *kind[T, P]) removeIn(ws *Workspace, holder, id string, opt ViewOptions) (*Result, error) {
	if err := k.checkFields(opt); err != nil {
		return nil, err
	}
	rec, token, err := k.loadIn(ws, holder, id)
	if err != nil {
		return nil, err
	}
	if err := ws.refused(k.Name, id); err != nil {
		return nil, err
	}
	res, err := k.result(OutcomeDeleted, rec, nil, opt)
	if err != nil {
		return nil, err
	}
	return res, ws.Store.Remove(k.Path(id), id, token, k.keys(rec))
}

// referenceScans find the records that refer to an ID. Each record type
// that refers to others adds its scan here; delete refuses while any finds
// a referrer.
var referenceScans = []func(ws *Workspace, id string) ([]string, error){usersReferringTo, peopleAffiliatedTo, interactionsReferringTo, tasksReferringTo, dealsReferringTo, projectsReferringTo, notesReferringTo}

// referrers lists the records that refer to id, sorted.
func (ws *Workspace) referrers(id string) ([]string, error) {
	var out []string
	ws = ws.strictly()
	for _, scan := range referenceScans {
		ids, err := scan(ws, id)
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
	}
	slices.Sort(out)
	return out, nil
}

// refused is REFERENCED when other records refer to id.
func (ws *Workspace) refused(name, id string) error {
	refs, err := ws.referrers(id)
	if err != nil || len(refs) == 0 {
		return err
	}
	hint := "change or delete the records in details.referrers first"
	if name == "interaction" && slices.ContainsFunc(refs, func(r string) bool { return strings.HasPrefix(r, "tk_") }) {
		hint = "delete the tasks in details.referrers, then the interaction, then create the interaction and those tasks again"
	}
	return newError(CodeReferenced, hint,
		"the %s %s is referred to by %d record(s)", name, id, len(refs)).with("referrers", refs)
}

// ListOptions are what a list takes.
type ListOptions struct {
	// Limit is the most records a page holds; 0 returns them all.
	Limit int
	// Cursor continues from a previous page's NextCursor.
	Cursor string
	// All includes records that are not live.
	All  bool
	View ViewOptions
}

// DefaultLimit is how many records a list returns when no limit is given.
const DefaultLimit = 50

// ListResult is one page of a list.
type ListResult struct {
	// Filters is every filter the list applied: the holder limit, the
	// live-only default and the ones the caller gave.
	Filters map[string]any
	Data    []View
	// Total counts every record the filters let through, on all pages.
	Total int
	// NextCursor continues the list; nil on the last page.
	NextCursor *string
}

// cursor is what a list's cursor holds: the last sort value and ID, and a
// fingerprint of the type, the filters and the order.
type cursor struct {
	Key         string `json:"k"`
	ID          string `json:"i"`
	Fingerprint string `json:"f"`
}

func badCursor(why string) *Error {
	return newError(CodeInvalidValue, "pass the next_cursor of the previous page with the same filters, or no --cursor to start again",
		"the cursor %s", why).with("field", "cursor")
}

// decodeCursor reads a list's cursor: nil for none, and a cursor that is not
// Kravis's, or belongs to another list, is INVALID_VALUE.
func decodeCursor(raw, fingerprint string) (*cursor, error) {
	if raw == "" {
		return nil, nil
	}
	dec, err := base64.RawURLEncoding.DecodeString(raw)
	from := &cursor{}
	if err != nil || json.Unmarshal(dec, from) != nil || from.ID == "" {
		return nil, badCursor("is not one Kravis gave")
	}
	if from.Fingerprint != fingerprint {
		return nil, badCursor("belongs to another list, filters or order")
	}
	return from, nil
}

func (c cursor) encode() string {
	enc, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(enc)
}

// checkPage refuses a limit or a cursor a list cannot take.
func checkPage(opt ListOptions) error {
	switch {
	case opt.Limit < 0:
		return InvalidFlag("limit", nil, "--limit must be 0 or more, not %d", opt.Limit)
	case opt.Limit == 0 && opt.Cursor != "":
		return newError(CodeInvalidValue, "drop --cursor, or give a --limit above 0",
			"--limit 0 returns every record, so it takes no --cursor").with("field", "cursor")
	}
	return nil
}

func (k *kind[T, P]) fingerprint(opt ListOptions, filters string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|all=%t|%s", k.Name, k.Order, opt.All, filters)))
	return hex.EncodeToString(sum[:6])
}

type row[P any] struct {
	key, id string
	rec     P
}

// cmp orders two rows: by sort key (reversed when the type sorts newest
// first), then by ID.
func (k *kind[T, P]) cmp(a, b row[P]) int {
	c := strings.Compare(a.key, b.key)
	if k.desc {
		c = -c
	}
	if c != 0 {
		return c
	}
	return strings.Compare(a.id, b.id)
}

// list returns one page of the type's records in its default order: live
// ones only, unless All, and for a holder only its records. keep, when not nil, drops the records it refuses;
// filters names what keep does (see applied): the list echoes them, and a cursor only continues the same list.
func (k *kind[T, P]) list(ws *Workspace, holder string, opt ListOptions, keep func(P) bool, filters map[string]any) (*ListResult, error) {
	if err := checkPage(opt); err != nil {
		return nil, err
	}
	if err := k.checkFields(opt.View); err != nil {
		return nil, err
	}
	shown := applied("holder", holder, "live", !opt.All)
	maps.Copy(shown, filters)
	raw, _ := json.Marshal(shown) // map keys are sorted
	fp := k.fingerprint(opt, string(raw))
	from, err := decodeCursor(opt.Cursor, fp)
	if err != nil {
		return nil, err
	}
	rows, err := k.scan(ws)
	if err != nil {
		return nil, err
	}
	rows = slices.DeleteFunc(rows, func(r row[P]) bool {
		return (!opt.All && !k.live(r.rec)) || (holder != "" && k.holder != nil && k.holder(r.rec) != holder) || (keep != nil && !keep(r.rec))
	})
	slices.SortFunc(rows, k.cmp)
	res := &ListResult{Data: []View{}, Total: len(rows), Filters: shown}
	start := 0
	if from != nil {
		start = len(rows)
		if i := slices.IndexFunc(rows, func(r row[P]) bool { return k.cmp(r, row[P]{key: from.Key, id: from.ID}) > 0 }); i >= 0 {
			start = i
		}
	}
	end := len(rows)
	if opt.Limit > 0 && opt.Limit < end-start {
		end = start + opt.Limit
		last := rows[end-1]
		next := cursor{last.key, last.id, fp}.encode()
		res.NextCursor = &next
	}
	for _, r := range rows[start:end] {
		v, err := k.view(r.rec, nil, opt.View)
		if err != nil {
			return nil, err
		}
		res.Data = append(res.Data, v)
	}
	return res, nil
}

// scan reads every record of the type. Files that vanish meanwhile are left
// out, and so are files that do not decode (kravis check reports them),
// except in a strict workspace, where they fail the scan with their path.
func (k *kind[T, P]) scan(ws *Workspace) ([]row[P], error) {
	paths, err := ws.Store.List("records/" + k.Dir + "/")
	if err != nil {
		return nil, err
	}
	var rows []row[P]
	for _, p := range paths {
		if !strings.HasSuffix(p, ".json") {
			continue
		}
		rec := P(new(T))
		if _, err := ws.Store.Load(p, rec); err != nil {
			if errors.Is(err, storage.ErrUnavailable) {
				return nil, err
			}
			if ws.strict && !errors.Is(err, storage.ErrNotFound) {
				return nil, newError(CodeStorageUnavailable,
					"run kravis check, repair that record, then repeat the command",
					"%s cannot be read, so the references to the record cannot be checked: %v", p, err).with("path", p)
			}
			continue
		}
		rows = append(rows, row[P]{key: Slug(k.sortKey(rec)), id: rec.Meta().ID, rec: rec})
	}
	return rows, nil
}

// touchedIf lists the names whose flag was given.
func touchedIf(pairs ...any) []string {
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1].(bool) {
			out = append(out, pairs[i].(string))
		}
	}
	return out
}

// audit reports the type's records that break its rules, values without
// their key and keys without their value. Records that do not decode are
// reported by Check itself.
func (k *kind[T, P]) audit(st *storage.Store, add addFinding) error {
	paths, err := st.List("records/" + k.Dir + "/")
	if err != nil {
		return err
	}
	byID := map[string]P{}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".json") {
			continue
		}
		data, _, err := st.Raw(p)
		rec := P(new(T))
		if err != nil || storage.Decode(data, &anyRecord{}) != nil {
			continue // unreadable, missing or not a record at all: Check reports it
		}
		if err := storage.Decode(data, rec); err != nil {
			add(FindingInvalidRecord, p, "a field has the wrong type: %v", err)
			continue
		}
		if rec.Meta().ID == "" {
			continue // Check reports a record without an id as malformed
		}
		var flags struct{ Live *bool }
		if json.Unmarshal(data, &flags) != nil || flags.Live == nil {
			add(FindingInvalidRecord, p, "the record has no live flag (true or false)")
		}
		id := rec.Meta().ID
		for _, problem := range slices.Concat(metaProblems(rec.Meta()), k.problems(rec)) {
			add(FindingInvalidRecord, p, "%s", problem)
		}
		if id != "" && k.Path(id) != p {
			add(FindingInvalidRecord, p, "the ID %q does not match the file name", id)
			continue
		}
		byID[id] = rec
		for _, key := range k.keys(rec) {
			switch owner, err := st.KeyOwner(key); {
			case errors.Is(err, storage.ErrNotFound):
				add(FindingValueWithoutKey, p, "%s %q has no deduplication key", key.Kind, key.Value)
			case err != nil:
				add(FindingValueWithoutKey, p, "the key of %s %q cannot be read: %v", key.Kind, key.Value, err)
			case owner != id:
				add(FindingValueWithoutKey, p, "%s %q is also on this record, but its key belongs to %s", key.Kind, key.Value, owner)
			}
		}
	}
	for _, kind := range k.keyKinds {
		keyPaths, err := st.List("keys/" + kind + "/")
		if err != nil {
			return err
		}
		for _, kp := range keyPaths {
			var f struct {
				Key    string `json:"key"`
				Record string `json:"record"`
			}
			data, _, err := st.Raw(kp)
			if err == nil {
				err = json.Unmarshal(data, &f)
			}
			if err != nil {
				add(FindingKeyWithoutValue, kp, "the key file cannot be read: %v", err)
				continue
			}
			key := storage.Key{Kind: kind, Value: f.Key}
			rec, ok := byID[f.Record]
			switch {
			case key.Path() != kp:
				add(FindingKeyWithoutValue, kp, "the key file is not at the path of its key %q", f.Key)
			case !ok:
				add(FindingKeyWithoutValue, kp, "%s %q points to %s, which does not exist", kind, f.Key, f.Record)
			case !slices.Contains(k.keys(rec), key):
				add(FindingKeyWithoutValue, kp, "%s %q points to %s, which does not have it", kind, f.Key, f.Record)
			}
		}
	}
	return nil
}
