package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/diegomarino/kravis/internal/storage"
)

// SearchTypes are the record types search reads, in the order docs/cli.md
// lists them. Users are not searched.
var SearchTypes = []string{"person", "organization", "interaction", "task", "deal", "project", "note"}

// excerptWindow is how many characters of a long text an excerpt shows.
const excerptWindow = 80

// SearchOptions are what a search takes.
type SearchOptions struct {
	Text string
	// Types narrows the search to these types; none searches them all.
	Types []string
	// Holder limits work records to one holder's; "" is no limit. People
	// and organizations are shared and never limited.
	Holder string
	// Page holds the limit, the cursor and All (records that are not live).
	Page ListOptions
}

// searchField is one searched field of a record: its text, or its several
// values; long texts give a window as their excerpt.
type searchField struct {
	name   string
	values []string
	long   bool
}

// searchMatch is a field that matched, with its excerpt.
type searchMatch struct {
	Field   string `json:"field"`
	Excerpt string `json:"excerpt"`
}

// hit is a record that matched, with what the CLI shows of it.
type hit struct {
	key, id string
	view    View
}

// searchKind finds the records of one type that match. fields lists a
// record's searched fields given its sidecar text, and finish completes its
// compact view; text is read only when hasText.
func searchKind[T any, P interface {
	*T
	storage.Record
}](ws *Workspace, k *kind[T, P], o SearchOptions, words []string, hasText bool,
	fields func(rec P, text string) []searchField, finish func(v View, text string) (View, error),
) (out []hit, hidden int, err error) {
	rows, err := k.scan(ws)
	if err != nil {
		return nil, 0, err
	}
	// --fields names that this type does not define belong to other types.
	vo := o.Page.View
	vo.Fields = nil
	own := slices.Concat(storage.FieldNames(P(new(T))), k.extraFields)
	for _, f := range o.Page.View.Fields {
		if slices.Contains(own, f) {
			vo.Fields = append(vo.Fields, f)
		}
	}
	for _, r := range rows {
		if o.Holder != "" && k.holder != nil && k.holder(r.rec) != o.Holder {
			continue
		}
		notLive := !o.Page.All && !k.live(r.rec)
		var text string
		if hasText {
			if text, err = ws.Store.Text(k.Path(r.id)); err != nil {
				return nil, 0, err
			}
		}
		matches, ok := matchFields(fields(r.rec, text), words)
		if !ok {
			continue
		}
		if notLive {
			hidden++
			continue
		}
		v, err := k.view(r.rec, nil, vo)
		if err == nil {
			v, err = finish(v, text)
		}
		if err != nil {
			return nil, 0, err
		}
		meta := r.rec.Meta()
		updated := meta.UpdatedAt
		if updated.IsZero() {
			updated = meta.CreatedAt
		}
		updated = updated.UTC()
		out = append(out, hit{
			key: updated.Format("2006-01-02T15:04:05.000000000"), id: r.id,
			view: searchView(k.Name, v, updated, matches),
		})
	}
	return out, hidden, nil
}

// searchView is a hit: its type, the compact view, updated_at and matches.
func searchView(typ string, v View, updated time.Time, matches []searchMatch) View {
	raw := func(x any) json.RawMessage { b, _ := json.Marshal(x); return b }
	out := View{{Name: "type", Value: raw(typ)}}
	for _, f := range v {
		if f.Name != "updated_at" { // --full shows the record's own
			out = append(out, f)
		}
	}
	return append(out, storage.Field{Name: "updated_at", Value: raw(updated)}, storage.Field{Name: "matches", Value: raw(matches)})
}

func plain(v View, _ string) (View, error) { return v, nil }

// hitText adds a sidecar text to a hit's view when --full or --fields asks for it.
func hitText(opt ViewOptions, name string) func(View, string) (View, error) {
	return func(v View, text string) (View, error) {
		if wantsText(opt, name) {
			v = withField(v, name, text)
		}
		return v, nil
	}
}

// Search finds records by their own text, in every type or in the given
// ones, newest update first (docs/cli.md, Search).
func (ws *Workspace) Search(o SearchOptions) (*ListResult, error) {
	words := strings.Fields(o.Text)
	text := strings.Join(words, " ")
	folded := make([]string, 0, len(words))
	for _, w := range words {
		if f := fold(w); f != "" {
			folded = append(folded, f)
		}
	}
	if len(folded) == 0 {
		return nil, newError(CodeInvalidValue, "give at least one word to search for", "the text has no words").with("field", "text")
	}
	for _, t := range o.Types {
		if !slices.Contains(SearchTypes, t) {
			return nil, InvalidFlag("type", SearchTypes, "%q is not a searchable type", t)
		}
	}
	if err := checkPage(o.Page); err != nil {
		return nil, err
	}
	var types []string
	for _, t := range SearchTypes {
		if len(o.Types) == 0 || slices.Contains(o.Types, t) {
			types = append(types, t)
		}
	}
	if err := checkSearchFields(types, o.Page.View.Fields); err != nil {
		return nil, err
	}
	shown := applied("holder", o.Holder, "live", !o.Page.All, "text", text)
	if len(o.Types) > 0 {
		shown["type"] = slices.DeleteFunc(slices.Clone(types), func(t string) bool { return !slices.Contains(o.Types, t) })
	}
	raw, _ := json.Marshal(shown) // map keys are sorted
	sum := sha256.Sum256([]byte("search|all=" + strconv.FormatBool(o.Page.All) + "|" + string(raw)))
	fp := hex.EncodeToString(sum[:6])
	from, err := decodeCursor(o.Page.Cursor, fp)
	if err != nil {
		return nil, err
	}

	cache := map[string]string{}
	hidden := 0
	run := func(found []hit, n int, err error) ([]hit, error) { hidden += n; return found, err }
	sources := map[string]func() ([]hit, error){
		"person": func() ([]hit, error) {
			return run(searchKind(ws, people, o, folded, false, func(p *Person, _ string) []searchField {
				return []searchField{{name: "name", values: []string{p.Name}}, {name: "emails", values: p.Emails}}
			}, plain))
		},
		"organization": func() ([]hit, error) {
			return run(searchKind(ws, organizations, o, folded, false, func(g *Organization, _ string) []searchField {
				return []searchField{{name: "name", values: []string{g.Name}}, {name: "domains", values: g.Domains}}
			}, plain))
		},
		"interaction": func() ([]hit, error) {
			return run(searchKind(ws, interactions, o, folded, true, func(i *Interaction, text string) []searchField {
				return []searchField{{name: "subject", values: []string{i.Subject}}, {name: "summary", values: []string{text}, long: true}}
			}, hitText(o.Page.View, "summary")))
		},
		"task": func() ([]hit, error) {
			return run(searchKind(ws, tasks, o, folded, true, func(t *Task, text string) []searchField {
				return []searchField{{name: "title", values: []string{t.Title}}, {name: "description", values: []string{text}, long: true}}
			}, func(v View, text string) (View, error) {
				v, err := ws.withTaskDeal(v, cache)
				if err != nil {
					return nil, err
				}
				return hitText(o.Page.View, "description")(v, text)
			}))
		},
		"deal": func() ([]hit, error) {
			return run(searchKind(ws, deals, o, folded, true, func(d *Deal, text string) []searchField {
				return []searchField{{name: "name", values: []string{d.Name}}, {name: "description", values: []string{text}, long: true}}
			}, hitText(o.Page.View, "description")))
		},
		"project": func() ([]hit, error) {
			return run(searchKind(ws, projects, o, folded, true, func(p *Project, text string) []searchField {
				return []searchField{{name: "name", values: []string{p.Name}}, {name: "description", values: []string{text}, long: true}}
			}, hitText(o.Page.View, "description")))
		},
		"note": func() ([]hit, error) {
			return run(searchKind(ws, notes, o, folded, true, func(_ *Note, text string) []searchField {
				return []searchField{{name: "text", values: []string{text}, long: true}}
			}, func(v View, text string) (View, error) { return noteView(v, text, wantsText(o.Page.View, "text")), nil }))
		},
	}
	var hits []hit
	for _, t := range types {
		found, err := sources[t]()
		if err != nil {
			return nil, err
		}
		hits = append(hits, found...)
	}
	// Newest update first, the ID breaking ties.
	cmp := func(a, b hit) int {
		if c := strings.Compare(b.key, a.key); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	}
	slices.SortFunc(hits, cmp)

	res := &ListResult{Data: []View{}, Total: len(hits), Filters: shown}
	if hidden > 0 {
		res.Warnings = []Warning{{Code: WarnNotLiveHidden,
			Message: fmt.Sprintf("hits that are not live were left out (%d): add --all to include them", hidden),
			Details: map[string]any{"count": hidden}}}
	}
	start := 0
	if from != nil {
		start = len(hits)
		if i := slices.IndexFunc(hits, func(h hit) bool { return cmp(h, hit{key: from.Key, id: from.ID}) > 0 }); i >= 0 {
			start = i
		}
	}
	end := len(hits)
	if o.Page.Limit > 0 && o.Page.Limit < end-start {
		end = start + o.Page.Limit
		next := cursor{hits[end-1].key, hits[end-1].id, fp}.encode()
		res.NextCursor = &next
	}
	for _, h := range hits[start:end] {
		res.Data = append(res.Data, h.view)
	}
	return res, nil
}

// checkSearchFields refuses a --fields name that none of the searched types
// defines; a name that only some define widens only their hits.
func checkSearchFields(types, fields []string) error {
	names := map[string][]string{
		"person":       storage.FieldNames(new(Person)),
		"organization": storage.FieldNames(new(Organization)),
		"interaction":  slices.Concat(storage.FieldNames(new(Interaction)), interactions.extraFields),
		"task":         slices.Concat(storage.FieldNames(new(Task)), tasks.extraFields),
		"deal":         slices.Concat(storage.FieldNames(new(Deal)), deals.extraFields),
		"project":      slices.Concat(storage.FieldNames(new(Project)), projects.extraFields),
		"note":         slices.Concat(storage.FieldNames(new(Note)), notes.extraFields),
	}
	var allowed []string
	for _, t := range types {
		allowed = append(allowed, names[t]...)
	}
	slices.Sort(allowed)
	allowed = slices.Compact(allowed)
	for _, f := range fields {
		if !slices.Contains(allowed, f) {
			return newError(CodeUnknownField, "", "no searched type has a field %q", f).with("field", f).with("allowed", allowed)
		}
	}
	return nil
}

// matchFields reports whether every word is in some field, and the fields
// that hold at least one word with their excerpts, in field order.
func matchFields(fields []searchField, words []string) ([]searchMatch, bool) {
	folded := make([][]foldedText, len(fields))
	for i, f := range fields {
		for _, v := range f.values {
			folded[i] = append(folded[i], foldText(v))
		}
	}
	for _, w := range words {
		if !slices.ContainsFunc(folded, func(vs []foldedText) bool {
			return slices.ContainsFunc(vs, func(v foldedText) bool { return strings.Contains(v.text, w) })
		}) {
			return nil, false
		}
	}
	var matches []searchMatch
	for i, f := range fields {
		for j, v := range folded[i] {
			if at, size := v.first(words); at >= 0 {
				excerpt := f.values[j]
				if f.long {
					excerpt = v.window(excerpt, at, size)
				}
				matches = append(matches, searchMatch{f.name, excerpt})
				break
			}
		}
	}
	return matches, true
}

// foldedText is a text folded rune by rune, with the position in the
// original of each folded rune, so an excerpt cuts the original.
type foldedText struct {
	text   string
	origin []int // origin[i] is the original rune index of folded rune i
}

func foldText(s string) foldedText {
	var b strings.Builder
	var origin []int
	n := 0
	for _, r := range s {
		f := fold(string(r))
		b.WriteString(f)
		for range utf8.RuneCountInString(f) {
			origin = append(origin, n)
		}
		n++
	}
	return foldedText{b.String(), origin}
}

// first finds the earliest word in the text: its folded rune offset and
// length, or -1.
func (f foldedText) first(words []string) (at, size int) {
	best := -1
	for _, w := range words {
		if i := strings.Index(f.text, w); i >= 0 && (best < 0 || i < best) {
			best, size = i, utf8.RuneCountInString(w)
		}
	}
	if best < 0 {
		return -1, 0
	}
	return utf8.RuneCountInString(f.text[:best]), size
}

// window cuts excerptWindow characters of the original text around the
// match, with … where it is cut.
func (f foldedText) window(orig string, at, size int) string {
	runes := []rune(orig)
	if len(runes) <= excerptWindow {
		return orig
	}
	from, to := f.origin[at], f.origin[at+size-1]+1
	start := min(max(from+(to-from)/2-excerptWindow/2, 0), len(runes)-excerptWindow)
	end := start + excerptWindow
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
