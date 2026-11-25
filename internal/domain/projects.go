package domain

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/diegomarino/kravis/internal/storage"
)

// Project is a piece of work of a holder, tied to at most one deal. Its
// description lives in the sidecar; it is live until it is closed.
type Project struct {
	storage.Base
	Holder  string      `json:"holder"`
	Name    string      `json:"name"`
	Deal    string      `json:"deal,omitempty"`
	Related storage.IDs `json:"related,omitempty"`
	Live    bool        `json:"live"`
}

var projects = newKind(kind[Project, *Project]{
	Type: Type{Name: "project", Dir: "projects", Prefix: "pr_",
		Order: "name compared as its slug; then the ID"},
	compact:     []compactField{{"id", ""}, {"name", ""}, {"live", "false"}, {"holder", ""}, {"deal", ""}},
	sortKey:     func(p *Project) string { return p.Name },
	holder:      func(p *Project) string { return p.Holder },
	live:        func(p *Project) bool { return p.Live },
	extraFields: []string{"description"},
	rules:       projectRules,
})

// relatedRules reports what is wrong with a list of related records.
func relatedRules(related []string) []string {
	var out []string
	for n, r := range related {
		switch {
		case !hasID("pe_", r) && !hasID("org_", r) && !hasID("dl_", r) && !hasID("pr_", r):
			out = append(out, "the related record "+quote(r)+" is not a person, organization, deal or project ID")
		case slices.Contains(related[:n], r):
			out = append(out, "the related record "+r+" is repeated")
		}
	}
	return out
}

// projectRules reports what is wrong inside a project record.
func projectRules(p *Project) []string {
	var out []string
	if !hasID("org_", p.Holder) {
		out = append(out, "the holder "+quote(p.Holder)+" is not an organization ID (org_<slug>)")
	}
	if strings.TrimSpace(p.Name) == "" {
		out = append(out, "a project needs a name")
	}
	if p.Deal != "" && !hasID("dl_", p.Deal) {
		out = append(out, "the deal "+quote(p.Deal)+" is not a deal ID (dl_<slug>)")
	}
	return append(out, relatedRules(p.Related)...)
}

func holderMismatch(field, id, other, holder string) *Error {
	return newError(CodeHolderMismatch, "link only records of your own holder",
		"%s belongs to %s, not to %s", id, other, holder).with("field", field)
}

// holderDeal loads a deal that a field names and checks it is the holder's
// (WORK-INV-02, INTERACTIONS-INV-05, WORKSPACE-INV-02).
func (ws *Workspace) holderDeal(holder, field, ref string) (*Deal, error) {
	d, err := loadLinked(ws, field, deals, ref)
	if err != nil {
		return nil, err
	}
	if d.Holder != holder {
		return nil, holderMismatch(field, ref, d.Holder, holder)
	}
	return d, nil
}

// holderProject is holderDeal for a project.
func (ws *Workspace) holderProject(holder, field, ref string) (*Project, error) {
	p, err := loadLinked(ws, field, projects, ref)
	if err != nil {
		return nil, err
	}
	if p.Holder != holder {
		return nil, holderMismatch(field, ref, p.Holder, holder)
	}
	return p, nil
}

// relatedID turns a related record given as an ID or a person's email into
// its ID, without looking at the record.
func (ws *Workspace) relatedID(field, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "dl_") || strings.HasPrefix(ref, "pr_") {
		return ref, nil
	}
	if strings.HasPrefix(ref, "org_") || strings.HasPrefix(ref, "pe_") || strings.Contains(ref, "@") {
		return ws.participantID(field, ref)
	}
	return "", invalidValue(field, "give a person (ID or email), organization, deal or project ID",
		"%q is not a person, an organization, a deal or a project", ref).with("value", ref)
}

// checkRelated resolves the related records given and checks that they
// exist and that deals and projects are the holder's (WORK-INV-08).
func (ws *Workspace) checkRelated(holder string, refs []string) ([]string, error) {
	var out []string
	for _, ref := range refs {
		id, err := ws.relatedID("related", ref)
		if err != nil {
			return nil, err
		}
		switch {
		case strings.HasPrefix(id, "dl_"):
			_, err = ws.holderDeal(holder, "related", id)
		case strings.HasPrefix(id, "pr_"):
			_, err = ws.holderProject(holder, "related", id)
		case strings.HasPrefix(id, "org_"):
			_, err = loadLinked(ws, "related", organizations, id)
		default:
			_, err = loadLinked(ws, "related", people, id)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return sortedIDs(out), nil
}

// relatedToDrop resolves the related records to remove, which need not exist.
func (ws *Workspace) relatedToDrop(refs []string) ([]string, error) {
	var out []string
	for _, ref := range refs {
		id, err := ws.relatedID("remove-related", ref)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// ProjectInput is what project create takes.
type ProjectInput struct {
	Name        string
	Deal        string
	Related     []string
	Description string
}

// CreateProject creates a live project of the actor's holder, tied to at
// most one deal of that holder (WORK-INV-02). The ID is pr_<slug>, with -2
// and SIMILAR_EXISTS on a collision.
func (ws *Workspace) CreateProject(actorID string, in ProjectInput, opt ViewOptions) (*Result, error) {
	if err := projects.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	holder := actor.Holder
	name := strings.TrimSpace(in.Name)
	if Slug(name) == "" {
		return nil, invalidValue("name", "give --name with letters or digits", "a project needs a name")
	}
	rec := &Project{Holder: holder, Name: name, Live: true}
	if deal := strings.TrimSpace(in.Deal); deal != "" {
		if _, err := ws.holderDeal(holder, "deal", deal); err != nil {
			return nil, err
		}
		rec.Deal = deal
	}
	if rec.Related, err = ws.checkRelated(holder, in.Related); err != nil {
		return nil, err
	}
	if in.Description != "" {
		rec.Text = &in.Description
	}
	_, similar, err := projects.create(ws, actorID, rec, Slug(name))
	if err != nil {
		return nil, err
	}
	var warnings []Warning
	if similar != "" {
		if other, _, err := projects.load(ws, similar); err == nil && other.Holder == holder {
			warnings = append(warnings, similarExists("project", rec.ID, similar))
		}
	}
	touched := touchedIf("deal", rec.Deal != "", "related", len(rec.Related) > 0, "description", in.Description != "")
	return ws.finishTask(opt, in.Description != "")(projects.result(OutcomeCreated, rec, touched, opt, warnings...))
}

// ProjectChange is what project update takes; nil leaves a field as it is.
// Whether a project is live changes only with close and reopen.
type ProjectChange struct {
	Name          *string
	Deal          *string // "" clears it
	Description   *string // "" clears it
	Related       []string
	RemoveRelated []string
}

// UpdateProject changes a project of the actor's holder; another holder's
// is NOT_FOUND. Its deal can change at any time: tasks take theirs from it.
func (ws *Workspace) UpdateProject(actorID, id string, ch ProjectChange, opt ViewOptions) (*Result, error) {
	if err := projects.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := projects.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	var name, deal string
	if ch.Name != nil {
		if name = strings.TrimSpace(*ch.Name); Slug(name) == "" {
			return nil, invalidValue("name", "give a name with letters or digits", "a project needs a name")
		}
	}
	if ch.Deal != nil {
		if deal = strings.TrimSpace(*ch.Deal); deal != "" {
			if _, err := ws.holderDeal(actor.Holder, "deal", deal); err != nil {
				return nil, err
			}
		}
	}
	add, err := ws.checkRelated(actor.Holder, ch.Related)
	if err != nil {
		return nil, err
	}
	drop, err := ws.relatedToDrop(ch.RemoveRelated)
	if err != nil {
		return nil, err
	}
	touched := touchedIf("name", ch.Name != nil, "deal", ch.Deal != nil, "description", ch.Description != nil,
		"related", len(add)+len(drop) > 0)
	rec, changed, err := projects.updateIn(ws, actor.Holder, actorID, id, func(p *Project) error {
		if ch.Name != nil {
			p.Name = name
		}
		if ch.Deal != nil {
			p.Deal = deal
		}
		p.Related = slices.DeleteFunc(sortedIDs(slices.Concat(p.Related, add)), func(r string) bool { return slices.Contains(drop, r) })
		if ch.Description != nil {
			cur, err := ws.Store.Text(projects.Path(id))
			if err != nil {
				return err
			}
			if cur != *ch.Description {
				p.Text = ch.Description
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ws.finishTask(opt, ch.Description != nil)(projects.result(outcome(changed), rec, touched, opt))
}

// liveTasksOfProject lists the live tasks that have the project as parent.
func (ws *Workspace) liveTasksOfProject(id string) ([]string, error) {
	return scanTasks(ws, func(t *Task) bool { return t.Live && t.Project == id })
}

// canceledStatus is the first status of the canceled category.
func (ws *Workspace) canceledStatus(holder string) (string, error) {
	for _, s := range ws.ConfigFor(holder).Catalogs.TaskStatuses {
		if s.Category == statusCanceled {
			return s.Name, nil
		}
	}
	return "", invalidValue("cancel-tasks", "add a status of the canceled category to the task statuses in config.json",
		"the task status catalog has no status of the canceled category")
}

// CloseProject closes a project of the actor's holder (WORK-AC-01,
// WORK-INV-09). With live tasks it fails with REFERENCED and lists them;
// cancel moves each of them to the first canceled status first, so it can
// be repeated after a partial failure. Closing a closed project changes
// nothing.
func (ws *Workspace) CloseProject(actorID, id string, cancel bool, opt ViewOptions) (*Result, error) {
	if err := projects.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := projects.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	live, err := ws.liveTasksOfProject(id)
	if err != nil {
		return nil, err
	}
	var canceled []string
	if cancel {
		status, err := ws.canceledStatus(actor.Holder)
		if err != nil {
			return nil, err
		}
		for _, t := range live {
			res, err := ws.SetTaskStatus(actorID, t, status, ViewOptions{})
			if err != nil {
				return nil, err
			}
			if res.Outcome == OutcomeUpdated {
				canceled = append(canceled, t)
			}
		}
	} else if len(live) > 0 {
		return nil, newError(CodeReferenced, "finish or cancel these tasks first, or use --cancel-tasks",
			"the project %s has %d live task(s)", id, len(live)).with("referrers", live)
	}
	rec, changed, err := projects.updateIn(ws, actor.Holder, actorID, id, func(p *Project) error {
		p.Live = false
		return nil
	})
	if err != nil {
		return nil, err
	}
	res, err := ws.finishTask(opt, false)(projects.result(outcome(changed || len(canceled) > 0), rec, []string{"live"}, opt))
	if err != nil || !cancel {
		return res, err
	}
	if canceled == nil {
		canceled = []string{}
	}
	raw, _ := json.Marshal(canceled)
	res.Data = append(res.Data, storage.Field{Name: "canceled_tasks", Value: raw})
	return res, nil
}

// ReopenProject makes a closed project of the actor's holder live again.
func (ws *Workspace) ReopenProject(actorID, id string, opt ViewOptions) (*Result, error) {
	if err := projects.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	rec, changed, err := projects.updateIn(ws, actor.Holder, actorID, id, func(p *Project) error {
		p.Live = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ws.finishTask(opt, false)(projects.result(outcome(changed), rec, []string{"live"}, opt))
}

// DeleteProject deletes a project of the actor's holder that no record
// refers to; another holder's is NOT_FOUND.
func (ws *Workspace) DeleteProject(actorID, id string, opt ViewOptions) (*Result, error) {
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	var text string
	if wantsText(opt, "description") {
		if _, _, err := projects.loadIn(ws, actor.Holder, id); err != nil {
			return nil, err
		}
		if text, err = ws.Store.Text(projects.Path(id)); err != nil {
			return nil, err
		}
	}
	res, err := projects.removeIn(ws, actor.Holder, id, opt)
	if err != nil {
		return nil, err
	}
	res.Data = withField(res.Data, "description", text)
	return res, nil
}

// ProjectFilter is what project list narrows by.
type ProjectFilter struct {
	// Holder limits the list to one holder's projects; "" is no limit.
	Holder string
	Deal   string
}

// ListProjects returns a page of projects by name, live ones unless All.
func (ws *Workspace) ListProjects(f ProjectFilter, opt ListOptions) (*ListResult, error) {
	keep := func(p *Project) bool { return f.Deal == "" || p.Deal == f.Deal }
	res, err := projects.list(ws, f.Holder, opt, keep, fmt.Sprintf("holder=%s|deal=%s", f.Holder, f.Deal))
	if err != nil {
		return nil, err
	}
	if wantsText(opt.View, "description") {
		for n, v := range res.Data {
			if res.Data[n], err = ws.addText(v, "description"); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// GetProject returns the view of a project. A read limited to a holder
// finds only that holder's projects.
func (ws *Workspace) GetProject(holder, id string, opt ViewOptions) (*GetResult, error) {
	v, err := projects.getIn(ws, holder, id, opt)
	if err == nil && wantsText(opt, "description") {
		v, err = ws.addText(v, "description")
	}
	if err != nil {
		return nil, err
	}
	return &GetResult{Data: v, Filters: holderFilter(holder)}, nil
}

// projectsReferringTo finds the projects that have id as their holder,
// deal or a related record.
func projectsReferringTo(ws *Workspace, id string) ([]string, error) {
	return scanProjects(ws, func(p *Project) bool {
		return p.Holder == id || p.Deal == id || slices.Contains(p.Related, id)
	})
}

// projectsOf finds the projects whose holder is id.
func projectsOf(ws *Workspace, id string) ([]string, error) {
	return scanProjects(ws, func(p *Project) bool { return p.Holder == id })
}

func scanProjects(ws *Workspace, match func(*Project) bool) ([]string, error) {
	rows, err := projects.scan(ws)
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

// auditRelated reports the related records that do not exist
// (DANGLING_REFERENCE) or belong to another holder (HOLDER_MISMATCH).
func auditRelated(add addFinding, path, holder string, related []string,
	orgs map[string]stored[*Organization], pe map[string]stored[*Person], ds map[string]stored[*Deal], ps map[string]stored[*Project]) {
	for _, r := range related {
		switch {
		case hasID("org_", r):
			if _, ok := orgs[r]; !ok {
				add(FindingDanglingReference, path, "the related record %s does not exist", r)
			}
		case hasID("pe_", r):
			if _, ok := pe[r]; !ok {
				add(FindingDanglingReference, path, "the related record %s does not exist", r)
			}
		case hasID("dl_", r):
			auditHolderLink(add, path, "related record", r, holder, ds)
		case hasID("pr_", r):
			auditHolderLink(add, path, "related record", r, holder, ps)
		}
	}
}

type holdered interface{ holderOf() string }

func (d *Deal) holderOf() string    { return d.Holder }
func (p *Project) holderOf() string { return p.Holder }

// auditHolderLink reports a link to a deal or project that does not exist
// or belongs to another holder.
func auditHolderLink[P holdered](add addFinding, path, what, id, holder string, recs map[string]stored[P]) {
	switch r, ok := recs[id]; {
	case !ok:
		add(FindingDanglingReference, path, "the %s %s does not exist", what, id)
	case r.rec.holderOf() != holder:
		add(FindingHolderMismatch, path, "the %s %s belongs to %s, not to the holder %s", what, id, r.rec.holderOf(), holder)
	}
}

// auditProjectLinks reports what projects refer to: a holder or deal or
// related record that does not exist, and links to another holder.
func auditProjectLinks(st *storage.Store, add addFinding) error {
	ps, err := loadForCheck(st, projects)
	if err != nil || len(ps) == 0 {
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
	for _, id := range slices.Sorted(maps.Keys(ps)) {
		p := ps[id]
		switch o, ok := orgs[p.rec.Holder]; {
		case !hasID("org_", p.rec.Holder):
		case !ok:
			add(FindingDanglingReference, p.path, "the holder %s does not exist", p.rec.Holder)
		case !o.rec.Own:
			add(FindingInvalidRecord, p.path, "the holder %s is not an own organization", p.rec.Holder)
		}
		if hasID("dl_", p.rec.Deal) {
			auditHolderLink(add, p.path, "deal", p.rec.Deal, p.rec.Holder, ds)
		}
		auditRelated(add, p.path, p.rec.Holder, p.rec.Related, orgs, pe, ds, ps)
	}
	return nil
}
