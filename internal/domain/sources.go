package domain

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// WarnUnknownSource is the warning for a reference to a source that is not
// in the catalog.
const WarnUnknownSource = "UNKNOWN_SOURCE"

// SourceRef is a pointer to an item in a source: <source>:<kind>:<id>[@<scope>].
type SourceRef struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Scope  string `json:"scope,omitempty"`
}

// String is the canonical written form of the reference.
func (r SourceRef) String() string {
	s := r.Source + ":" + r.Kind + ":" + r.ID
	if r.Scope != "" {
		s += "@" + r.Scope
	}
	return s
}

// sourceRow is one row of the catalog of sources (docs/storage.md, Sources).
type sourceRow struct {
	source, kind string
	// scoped sources need the account after an @; their IDs hold no @.
	scoped bool
	// atInID says that the ID itself may hold an @ (Message-IDs, UIDs and
	// URLs do). Without a scope, any other source has no @ at all.
	atInID bool
	// identifies says that the reference identifies an interaction.
	identifies bool
	// canon makes the ID canonical; nil keeps it as given.
	canon func(id string) (string, error)
}

// sources is the catalog, in code because each source behaves in its own way.
// Adding a source is adding a row.
var sources = []sourceRow{
	{source: "email", kind: "message", atInID: true, identifies: true},
	{source: "ical", kind: "event", atInID: true, identifies: true},
	{source: "granola", kind: "meeting", identifies: true},
	{source: "slack", kind: "message", identifies: true, canon: twoPart("channel ID", "ts")},
	{source: "telegram", kind: "message", identifies: true, canon: twoPart("chat ID", "message ID")},
	{source: "gmail", kind: "message", scoped: true},
	{source: "gmail", kind: "thread", scoped: true},
	{source: "outlook", kind: "message", scoped: true},
	{source: "drive", kind: "file"},
	{source: "onedrive", kind: "file"},
	{source: "web", kind: "url", atInID: true, canon: canonicalURL},
}

func findSource(source, kind string) *sourceRow {
	i := slices.IndexFunc(sources, func(r sourceRow) bool { return r.source == source && r.kind == kind })
	if i < 0 {
		return nil
	}
	return &sources[i]
}

// twoPart checks an ID of the form <first>/<second>, both parts given.
func twoPart(first, second string) func(string) (string, error) {
	return func(id string) (string, error) {
		a, b, ok := strings.Cut(id, "/")
		if !ok || a == "" || b == "" || strings.Contains(b, "/") {
			return "", fmt.Errorf("%q is not <%s>/<%s>", id, first, second)
		}
		return id, nil
	}
}

// canonicalURL lowercases the scheme and host and drops the fragment.
func canonicalURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%q is not a URL with a scheme and a host", raw)
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	u.Fragment, u.RawFragment = "", ""
	return u.String(), nil
}

func badRef(raw, hint, format string, args ...any) *Error {
	return invalidValue("ref", hint, format, args...).with("value", raw)
}

// parseRef reads <source>:<kind>:<id>[@<scope>] into its canonical form. A
// source that is not in the catalog is accepted with the UNKNOWN_SOURCE
// warning, its shape unchecked.
func parseRef(raw string) (SourceRef, *Warning, error) {
	const shape = "write it as <source>:<kind>:<id>[@<scope>], such as email:message:<message-id>"
	source, rest, ok1 := strings.Cut(strings.TrimSpace(raw), ":")
	kind, id, ok2 := strings.Cut(rest, ":")
	ref := SourceRef{Source: strings.ToLower(strings.TrimSpace(source)), Kind: strings.ToLower(strings.TrimSpace(kind)), ID: id}
	if !ok1 || !ok2 || ref.Source == "" || ref.Kind == "" || ref.ID == "" {
		return ref, nil, badRef(raw, shape, "%q is not a source reference", raw)
	}
	row := findSource(ref.Source, ref.Kind)
	if row == nil {
		return ref, &Warning{Code: WarnUnknownSource,
			Message: fmt.Sprintf("the source %s:%s is not in the catalog, so the reference matches only an identical one", ref.Source, ref.Kind),
			Details: map[string]any{"ref": ref.String(), "source": ref.Source + ":" + ref.Kind}}, nil
	}
	if row.scoped {
		var scope string
		ref.ID, scope, _ = strings.Cut(id, "@")
		ref.Scope = strings.ToLower(strings.TrimSpace(scope))
		if ref.ID == "" || ref.Scope == "" {
			return ref, nil, badRef(raw, fmt.Sprintf("add the account: %s:%s:<id>@<account>", ref.Source, ref.Kind),
				"a %s:%s reference needs its account after an @", ref.Source, ref.Kind)
		}
	} else if !row.atInID && strings.Contains(id, "@") {
		return ref, nil, badRef(raw, fmt.Sprintf("write %s:%s:<id>, with no @", ref.Source, ref.Kind),
			"a %s:%s reference takes no scope", ref.Source, ref.Kind)
	}
	if row.canon != nil {
		c, err := row.canon(ref.ID)
		if err != nil {
			return ref, nil, badRef(raw, shape, "%v", err)
		}
		ref.ID = c
	}
	return ref, nil, nil
}

// identifying says whether a reference identifies an interaction: those of
// sources that say so, and every reference to an unknown source, by exact
// equality.
func identifying(r SourceRef) bool {
	row := findSource(r.Source, r.Kind)
	return row == nil || row.identifies
}

// parseRefs reads a list of references into a sorted list without repeats,
// and the warnings they carry.
func parseRefs(raw []string) ([]SourceRef, []Warning, error) {
	var refs []SourceRef
	var warnings []Warning
	for _, r := range raw {
		ref, w, err := parseRef(r)
		if err != nil {
			return nil, nil, err
		}
		if w != nil && !slices.ContainsFunc(warnings, func(x Warning) bool { return x.Details["ref"] == w.Details["ref"] }) {
			warnings = append(warnings, *w)
		}
		refs = append(refs, ref)
	}
	return sortRefs(refs), warnings, nil
}

func sortRefs(refs []SourceRef) []SourceRef {
	slices.SortFunc(refs, func(a, b SourceRef) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(refs)
}

// refRules reports stored references that are not canonical or repeat.
func refRules(refs []SourceRef) []string {
	var out []string
	for i, r := range refs {
		switch got, _, err := parseRef(r.String()); {
		case err != nil:
			out = append(out, fmt.Sprintf("the reference %q is not valid: %v", r.String(), err))
		case got != r:
			out = append(out, fmt.Sprintf("the reference %q is not in its canonical form %q", r.String(), got.String()))
		}
		if slices.Contains(refs[:i], r) {
			out = append(out, fmt.Sprintf("the reference %q is repeated", r.String()))
		}
	}
	return out
}
