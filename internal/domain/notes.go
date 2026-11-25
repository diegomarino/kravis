package domain

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"go.jetify.com/typeid"

	"github.com/diegomarino/kravis/internal/storage"
)

// excerptLength is how many characters of a note's first line its view shows.
const excerptLength = 80

// Note is what a holder knows about people, organizations, deals or
// projects. Its text lives in the sidecar and is always there. A note is
// always live: one that is no longer useful is deleted.
type Note struct {
	storage.Base
	Holder string `json:"holder"`
	// Subjects are people, organizations, deals and projects, at least one.
	Subjects storage.IDs `json:"subjects"`
	Live     bool        `json:"live"`
}

var notes = newKind(kind[Note, *Note]{
	Type: Type{Name: "note", Dir: "notes", Prefix: "nt_",
		Order: "created_at, newest first; then the ID",
		group: typeIDMonth},
	compact: []compactField{{"id", ""}, {"live", "false"}, {"holder", ""}, {"subjects", ""}, {"excerpt", ""}, {"created_at", ""}},
	// A fixed-width instant sorts as time does, once Slug turned its
	// punctuation into hyphens.
	sortKey:     func(n *Note) string { return n.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000") },
	desc:        true,
	holder:      func(n *Note) string { return n.Holder },
	form:        typeIDForm,
	live:        func(n *Note) bool { return n.Live },
	extraFields: []string{"text", "excerpt"},
	rules:       noteRules,
})

// noteRules reports what is wrong inside a note record (INTERACTIONS-INV-10).
func noteRules(n *Note) []string {
	var out []string
	if !hasID("org_", n.Holder) {
		out = append(out, "the holder "+quote(n.Holder)+" is not an organization ID (org_<slug>)")
	}
	if len(n.Subjects) == 0 {
		out = append(out, "a note needs at least one subject")
	}
	if !n.Live {
		out = append(out, "a note is always live")
	}
	return append(out, relatedRules("subject", n.Subjects)...)
}

// excerpt is the first line of a note's text, cut at 80 characters.
func excerpt(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) > excerptLength {
		line = string([]rune(line)[:excerptLength])
	}
	return line
}

// noteView adds a note's excerpt, computed from its text, right after its
// subjects, and its text when asked.
func noteView(v View, text string, withText bool) View {
	raw, _ := json.Marshal(excerpt(text))
	i := slices.IndexFunc(v, func(f storage.Field) bool { return f.Name == "subjects" })
	out := slices.Insert(slices.Clone(v), i+1, storage.Field{Name: "excerpt", Value: raw})
	if withText {
		raw, _ := json.Marshal(text)
		out = append(out, storage.Field{Name: "text", Value: raw})
	}
	return out
}

// noteShown completes the view of a note with its text from the sidecar.
func (ws *Workspace) noteShown(v View, withText bool) (View, error) {
	for _, f := range v {
		if f.Name == "id" {
			var id string
			if err := json.Unmarshal(f.Value, &id); err != nil {
				break
			}
			text, err := ws.Store.Text(notes.Path(id))
			return noteView(v, text, withText), err
		}
	}
	return v, nil
}

// finishNote completes the view of a write's result.
func (ws *Workspace) finishNote(opt ViewOptions, touched bool) func(*Result, error) (*Result, error) {
	return func(res *Result, err error) (*Result, error) {
		if err != nil {
			return res, err
		}
		res.Data, err = ws.noteShown(res.Data, touched || wantsText(opt, "text"))
		return res, err
	}
}

// NoteInput is what note create takes.
type NoteInput struct {
	Subjects []string
	Text     string
}

// CreateNote creates a note of the actor's holder about one or more
// people, organizations, deals or projects, the deals and projects of that
// holder (INTERACTIONS-INV-10, WORKSPACE-INV-01, -02). Its text is
// required.
func (ws *Workspace) CreateNote(actorID string, in NoteInput, opt ViewOptions) (*Result, error) {
	if err := notes.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if len(in.Subjects) == 0 {
		return nil, invalidValue("subject", "give --subject <person, organization, deal or project> at least once", "a note needs at least one subject")
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, invalidValue("text", "give --text-file <file>, or - for stdin", "a note needs text")
	}
	subjects, err := ws.checkRelated(actor.Holder, "subject", in.Subjects)
	if err != nil {
		return nil, err
	}
	tid, err := typeid.WithPrefix(strings.TrimSuffix(notes.Prefix, "_"))
	if err != nil {
		return nil, err
	}
	rec := &Note{Holder: actor.Holder, Subjects: subjects, Live: true}
	rec.ID, rec.Text = tid.String(), &in.Text
	if problems := notes.problems(rec); len(problems) > 0 {
		return nil, newError(CodeInvalidValue, "", "the note would be invalid: %s", problems[0])
	}
	if _, _, err := storage.Create(ws.Store, rec, actorID, notes.Path, notes.keys); err != nil {
		return nil, err
	}
	return ws.finishNote(opt, true)(notes.result(OutcomeCreated, rec, []string{"text"}, opt))
}

// NoteChange is what note update takes; nil leaves the text as it is.
type NoteChange struct {
	Text           *string
	Subjects       []string
	RemoveSubjects []string
}

// UpdateNote changes the text and the subjects of a note of the actor's
// holder; another holder's is NOT_FOUND. It never leaves a note without
// subjects.
func (ws *Workspace) UpdateNote(actorID, id string, ch NoteChange, opt ViewOptions) (*Result, error) {
	if err := notes.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := notes.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	if ch.Text != nil && strings.TrimSpace(*ch.Text) == "" {
		return nil, invalidValue("text", "a note needs text: delete it instead", "a note cannot have no text")
	}
	add, err := ws.checkRelated(actor.Holder, "subject", ch.Subjects)
	if err != nil {
		return nil, err
	}
	drop, err := ws.relatedToDrop("remove-subject", ch.RemoveSubjects)
	if err != nil {
		return nil, err
	}
	rec, changed, err := notes.updateIn(ws, actor.Holder, actorID, id, func(n *Note) error {
		n.Subjects = slices.DeleteFunc(sortedIDs(slices.Concat(n.Subjects, add)), func(s string) bool { return slices.Contains(drop, s) })
		if len(n.Subjects) == 0 {
			return invalidValue("remove-subject", "a note keeps at least one subject: add another with --subject, or delete the note",
				"the note %s would have no subject", id)
		}
		if ch.Text != nil {
			cur, err := ws.Store.Text(notes.Path(id))
			if err != nil {
				return err
			}
			if cur != *ch.Text {
				n.Text = ch.Text
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	touched := touchedIf("subjects", len(add)+len(drop) > 0, "text", ch.Text != nil)
	return ws.finishNote(opt, ch.Text != nil)(notes.result(outcome(changed), rec, touched, opt))
}

// DeleteNote deletes a note of the actor's holder, text included; another
// holder's is NOT_FOUND. Nothing refers to a note.
func (ws *Workspace) DeleteNote(actorID, id string, opt ViewOptions) (*Result, error) {
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if err := notes.checkFields(opt); err != nil {
		return nil, err
	}
	if _, _, err := notes.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	text, err := ws.Store.Text(notes.Path(id))
	if err != nil {
		return nil, err
	}
	res, err := notes.removeIn(ws, actor.Holder, id, opt)
	if err != nil {
		return nil, err
	}
	res.Data = noteView(res.Data, text, wantsText(opt, "text"))
	return res, nil
}

// NoteFilter is what note list narrows by.
type NoteFilter struct {
	// Holder limits the list to one holder's notes; "" is no limit.
	Holder string
}

// ListNotes returns a page of notes, newest first.
func (ws *Workspace) ListNotes(f NoteFilter, opt ListOptions) (*ListResult, error) {
	res, err := notes.list(ws, f.Holder, opt, nil, "")
	if err != nil {
		return nil, err
	}
	for n, v := range res.Data {
		if res.Data[n], err = ws.noteShown(v, wantsText(opt.View, "text")); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// GetNote returns the view of a note. A read limited to a holder finds
// only that holder's notes.
func (ws *Workspace) GetNote(holder, id string, opt ViewOptions) (*GetResult, error) {
	v, err := notes.getIn(ws, holder, id, opt)
	if err == nil {
		v, err = ws.noteShown(v, wantsText(opt, "text"))
	}
	if err != nil {
		return nil, err
	}
	return &GetResult{Data: v, Filters: holderFilter(holder)}, nil
}

// notesReferringTo finds the notes that have id as their holder or a
// subject.
func notesReferringTo(ws *Workspace, id string) ([]string, error) {
	return scanNotes(ws, func(n *Note) bool { return n.Holder == id || slices.Contains(n.Subjects, id) })
}

// notesOf finds the notes whose holder is id.
func notesOf(ws *Workspace, id string) ([]string, error) {
	return scanNotes(ws, func(n *Note) bool { return n.Holder == id })
}

func scanNotes(ws *Workspace, match func(*Note) bool) ([]string, error) {
	rows, err := notes.scan(ws)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if match(r.rec) {
			out = append(out, r.id)
		}
	}
	slices.Sort(out)
	return out, nil
}

// auditNoteLinks reports what notes refer to: a holder or subject that does
// not exist (DANGLING_REFERENCE), a deal or project of another holder, and
// a note without its text sidecar (INVALID_RECORD).
func auditNoteLinks(st *storage.Store, add addFinding) error {
	ns, err := loadForCheck(st, notes)
	if err != nil || len(ns) == 0 {
		return err
	}
	orgs, err := loadForCheck(st, organizations)
	if err != nil {
		return err
	}
	pe, err := loadForCheck(st, people)
	if err != nil {
		return err
	}
	ds, err := loadForCheck(st, deals)
	if err != nil {
		return err
	}
	ps, err := loadForCheck(st, projects)
	if err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(ns)) {
		n := ns[id]
		switch o, ok := orgs[n.rec.Holder]; {
		case !hasID("org_", n.rec.Holder):
		case !ok:
			add(FindingDanglingReference, n.path, "the holder %s does not exist", n.rec.Holder)
		case !o.rec.Own:
			add(FindingInvalidRecord, n.path, "the holder %s is not an own organization", n.rec.Holder)
		}
		auditRelated(add, n.path, n.rec.Holder, "subject", n.rec.Subjects, orgs, pe, ds, ps)
		switch text, err := st.Text(n.path); {
		case err != nil:
			return err
		case strings.TrimSpace(text) == "":
			add(FindingInvalidRecord, n.path, "the note has no text: its %s sidecar is missing or empty", storage.SidecarPath(n.path))
		}
	}
	return nil
}
