package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
}

// Path is where the record with the given ID lives.
func (t Type) Path(id string) string { return "records/" + t.Dir + "/" + id + ".json" }

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
	live    func(P) bool
	// keys are the record's natural keys (nil: none).
	keys func(P) []storage.Key
	// rules reports what is wrong with a record's fields, for writes and
	// check; problems adds its ID.
	rules func(P) []string
}

// problems reports what is wrong with a record: its ID, then its fields.
func (k *kind[T, P]) problems(rec P) []string {
	var out []string
	if id := rec.Meta().ID; !k.validID(id) {
		out = append(out, fmt.Sprintf("%q is not a %s ID (%s<slug>)", id, k.Name, k.Prefix))
	}
	return append(out, k.rules(rec)...)
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
	return &k
}

// slugID is the form of every slug ID after its prefix.
var slugID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (k *kind[T, P]) validID(id string) bool {
	rest, ok := strings.CutPrefix(id, k.Prefix)
	return ok && slugID.MatchString(rest)
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
	allowed := storage.FieldNames(P(new(T)))
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
	if err := k.checkFields(opt); err != nil {
		return nil, err
	}
	rec, _, err := k.load(ws, id)
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
	if !k.validID(id) {
		return nil, false, k.notFound(id)
	}
	seen := 0
	rec, err := storage.Update(ws.Store, k.Path(id), actor, func(p P) error {
		seen = p.Meta().Version
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
	if err := k.checkFields(opt); err != nil {
		return nil, err
	}
	rec, token, err := k.load(ws, id)
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
var referenceScans = []func(ws *Workspace, id string) ([]string, error){usersReferringTo}

// referrers lists the records that refer to id, sorted.
func (ws *Workspace) referrers(id string) ([]string, error) {
	var out []string
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
	return newError(CodeReferenced, "change or delete the records in details.referrers first",
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
	Data []View
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

func (k *kind[T, P]) fingerprint(opt ListOptions) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|all=%t", k.Name, k.Order, opt.All)))
	return hex.EncodeToString(sum[:6])
}

type row[P any] struct {
	key, id string
	rec     P
}

func (r row[P]) after(key, id string) bool { return r.key > key || (r.key == key && r.id > id) }

// list returns one page of the type's records in its default order: live
// ones only, unless All.
func (k *kind[T, P]) list(ws *Workspace, opt ListOptions) (*ListResult, error) {
	switch {
	case opt.Limit < 0:
		return nil, InvalidFlag("limit", nil, "--limit must be 0 or more, not %d", opt.Limit)
	case opt.Limit == 0 && opt.Cursor != "":
		return nil, newError(CodeInvalidValue, "drop --cursor, or give a --limit above 0",
			"--limit 0 returns every record, so it takes no --cursor").with("field", "cursor")
	}
	if err := k.checkFields(opt.View); err != nil {
		return nil, err
	}
	fp := k.fingerprint(opt)
	var from *cursor
	if opt.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(opt.Cursor)
		from = &cursor{}
		if err != nil || json.Unmarshal(raw, from) != nil || from.ID == "" {
			return nil, badCursor("is not one Kravis gave")
		}
		if from.Fingerprint != fp {
			return nil, badCursor("belongs to another list, filters or order")
		}
	}
	rows, err := k.scan(ws)
	if err != nil {
		return nil, err
	}
	rows = slices.DeleteFunc(rows, func(r row[P]) bool { return !opt.All && !k.live(r.rec) })
	slices.SortFunc(rows, func(a, b row[P]) int {
		if c := strings.Compare(a.key, b.key); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	})
	res := &ListResult{Data: []View{}, Total: len(rows)}
	start := 0
	if from != nil {
		start = len(rows)
		if i := slices.IndexFunc(rows, func(r row[P]) bool { return r.after(from.Key, from.ID) }); i >= 0 {
			start = i
		}
	}
	end := len(rows)
	if opt.Limit > 0 && start+opt.Limit < end {
		end = start + opt.Limit
		last := rows[end-1]
		raw, _ := json.Marshal(cursor{last.key, last.id, fp})
		next := base64.RawURLEncoding.EncodeToString(raw)
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

// scan reads every record of the type. Files that vanish meanwhile or do
// not decode are left out: kravis check reports the latter.
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
