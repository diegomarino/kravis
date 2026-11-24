package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.jetify.com/typeid"

	"github.com/diegomarino/kravis/internal/storage"
)

// KeyTaskOrigin is the natural key of a task made from an interaction: the
// interaction and the purpose.
const KeyTaskOrigin = "task-origin"

// A task starts in this status, and a task with an interaction and no
// purpose gets this purpose.
const (
	initialStatus  = "todo"
	defaultPurpose = "follow-up"
)

// Task is something to do, answered for by one human and possibly carried
// out by one of their agents. Its description lives in the sidecar.
type Task struct {
	storage.Base
	Holder string `json:"holder"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// Category and Live follow the status in the catalog.
	Category string `json:"category"`
	Live     bool   `json:"live"`
	// Assignee is a live human of the holder.
	Assignee string `json:"assignee"`
	// Agent, if any, is an agent whose handler is the assignee.
	Agent string `json:"agent,omitempty"`
	// From is the interaction the task comes from; it never changes.
	From    string `json:"from,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	// Due is a date in the workspace timezone.
	Due string `json:"due,omitempty"`
}

const dateLayout = "2006-01-02"

// taskForm is what a task ID looks like after tk_: a TypeID suffix, 26
// characters of base32 that start with 0-7.
var taskForm = regexp.MustCompile(`^[0-7][0-9a-hjkmnp-tv-z]{25}$`)

// taskMonth is the folder of a task: the month, in UTC, of the time in its
// TypeID, so it never depends on the workspace timezone.
func taskMonth(id string) string {
	tid, err := typeid.FromString(id)
	if err != nil {
		return ""
	}
	b := tid.UUIDBytes()
	var ms int64
	for _, x := range b[:6] {
		ms = ms<<8 | int64(x)
	}
	return time.UnixMilli(ms).UTC().Format("2006-01")
}

var tasks = newKind(kind[Task, *Task]{
	Type: Type{Name: "task", Dir: "tasks", Prefix: "tk_",
		Order: "due, soonest first, tasks without a due date last; then the ID",
		group: taskMonth},
	compact: []compactField{{"id", ""}, {"title", ""}, {"live", "false"}, {"holder", ""},
		{"status", ""}, {"category", ""}, {"assignee", ""}, {"agent", ""}, {"due", ""}},
	// Tasks without a due date sort after every date.
	sortKey:     func(t *Task) string { return firstOf(t.Due, "9999-99-99") },
	holder:      func(t *Task) string { return t.Holder },
	form:        taskForm,
	live:        func(t *Task) bool { return t.Live },
	keyKinds:    []string{KeyTaskOrigin},
	extraFields: []string{"description"},
	keys:        taskKeys,
	rules:       taskRules,
})

// taskKeys are the record's natural keys: its origin, when it has one.
func taskKeys(t *Task) []storage.Key {
	if t.From == "" {
		return nil
	}
	return []storage.Key{{Kind: KeyTaskOrigin, Value: "task|" + t.From + "|" + t.Purpose}}
}

// isLive says whether a status category counts as live: open and waiting do,
// done and canceled are kept but not live.
func isLive(category string) bool { return category == "open" || category == "waiting" }

// validDate says whether s is a date such as 2026-11-28.
func validDate(s string) bool {
	t, err := time.Parse(dateLayout, s)
	return err == nil && t.Format(dateLayout) == s
}

// taskRules reports what is wrong inside a task record, whatever else
// exists (WORK-INV-03..05, WORKSPACE-INV-06).
func taskRules(t *Task) []string {
	var out []string
	if !hasID("org_", t.Holder) {
		out = append(out, "the holder "+quote(t.Holder)+" is not an organization ID (org_<slug>)")
	}
	if strings.TrimSpace(t.Title) == "" {
		out = append(out, "a task needs a title")
	}
	if !slugID.MatchString(t.Status) {
		out = append(out, "the status "+quote(t.Status)+" is not a catalog value (a slug)")
	}
	switch {
	case !slices.Contains(statusCategories, t.Category):
		out = append(out, "the category "+quote(t.Category)+" is not one of "+strings.Join(statusCategories, ", "))
	case t.Live != isLive(t.Category):
		out = append(out, fmt.Sprintf("live is %t, but a task in category %s is live=%t", t.Live, t.Category, isLive(t.Category)))
	}
	if !hasID("us_", t.Assignee) {
		out = append(out, "the assignee "+quote(t.Assignee)+" is not a user ID (us_<key>)")
	}
	if t.Agent != "" && !hasID("us_", t.Agent) {
		out = append(out, "the agent "+quote(t.Agent)+" is not a user ID (us_<key>)")
	}
	if t.From != "" && !hasID("it_", t.From) {
		out = append(out, "the origin "+quote(t.From)+" is not an interaction ID (it_<date>-<slug>)")
	}
	switch {
	case t.From != "" && t.Purpose == "":
		out = append(out, "a task from an interaction needs a purpose")
	case t.Purpose != "" && !slugID.MatchString(t.Purpose):
		out = append(out, "the purpose "+quote(t.Purpose)+" is not a catalog value (a slug)")
	}
	if t.Due != "" && !validDate(t.Due) {
		out = append(out, "the due date "+quote(t.Due)+" is not a date such as 2026-11-28")
	}
	return out
}

// parseDate reads a date such as 2026-11-28; "" is no date.
func parseDate(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s != "" && !validDate(s) {
		return "", invalidValue(field, "use a date such as 2026-11-28", "%q is not a date", s).with("value", s)
	}
	return s, nil
}

// category is the catalog's category of a status for a holder.
func (ws *Workspace) category(holder, status string) (string, bool) {
	for _, s := range ws.ConfigFor(holder).Catalogs.TaskStatuses {
		if s.Name == status {
			return s.Category, true
		}
	}
	return "", false
}

func statusNames(c Config) []string {
	names := make([]string, len(c.Catalogs.TaskStatuses))
	for i, s := range c.Catalogs.TaskStatuses {
		names[i] = s.Name
	}
	return names
}

// checkStatus refuses a status that is not in the holder's catalog and
// returns its category (WORKSPACE-AC-13). stale says the status was not just
// given: it is the one the task already had.
func (ws *Workspace) checkStatus(holder, status string, stale bool) (string, error) {
	if cat, ok := ws.category(holder, status); ok {
		return cat, nil
	}
	allowed := statusNames(ws.ConfigFor(holder))
	e := invalidValue("status", "use one of the allowed statuses", "the status %q is not in the task status catalog", status).
		with("allowed", allowed).with("value", status)
	if stale {
		e.Message = fmt.Sprintf("the status %q is no longer in the task status catalog, so this task must get another one first", status)
		e.Hint = "run kravis task status <task> <status> with one of the allowed statuses"
	}
	return "", e
}

// checkPurpose refuses a purpose that is not in the holder's catalog.
func (ws *Workspace) checkPurpose(holder, purpose string) error {
	allowed := ws.ConfigFor(holder).Catalogs.Purposes
	if slices.Contains(allowed, purpose) {
		return nil
	}
	return invalidValue("purpose", "use one of the allowed purposes", "the purpose %q is not in the purpose catalog", purpose).
		with("allowed", allowed).with("value", purpose)
}

// checkAssignment is WORK-INV-03 and -04 for the holder's task: the assignee
// is a live human of the holder, and the agent, if any, is a live agent of
// the holder that the assignee handles. newAssignee and newAgent say which
// were just given; only those are judged again, so a task whose people were
// deactivated after it was finished can still be edited.
func (ws *Workspace) checkAssignment(holder, assignee, agent string, newAssignee, newAgent bool) error {
	if newAssignee {
		u, err := loadLinked(ws, "assignee", users, assignee)
		if err != nil {
			return err
		}
		switch {
		case u.Kind != KindHuman:
			return invalidValue("assignee", "the assignee is a human; an agent goes in --agent", "%s is an agent, and a task is assigned to a human", assignee)
		case u.Holder != holder:
			return newError(CodeHolderMismatch, "choose a human of the task's holder", "%s works for %s, not for %s", assignee, u.Holder, holder).
				with("field", "assignee")
		case !u.Live:
			return invalidValue("assignee", "choose a live human", "%s is not live", assignee)
		}
	}
	if agent == "" || (!newAssignee && !newAgent) {
		return nil
	}
	a, err := loadLinked(ws, "agent", users, agent)
	if err != nil {
		return err
	}
	if newAgent {
		switch {
		case a.Kind != KindAgent:
			return invalidValue("agent", "the assigned agent is an agent user", "%s is a human, and --agent takes an agent", agent)
		case a.Holder != holder:
			return newError(CodeHolderMismatch, "choose an agent of the task's holder", "%s works for %s, not for %s", agent, a.Holder, holder).
				with("field", "agent")
		case !a.Live:
			return invalidValue("agent", "choose a live agent", "%s is not live", agent)
		}
	}
	if a.Handler != assignee {
		if newAgent {
			return invalidValue("agent", "choose an agent that the assignee handles", "the agent %s is handled by %s, not by the assignee %s", agent, a.Handler, assignee).
				with("value", agent)
		}
		return invalidValue("assignee", `give --agent <an agent of the new assignee>, or --agent "" to clear it`,
			"the agent %s is handled by %s, not by the new assignee %s", agent, a.Handler, assignee).with("value", assignee)
	}
	return nil
}

// TaskInput is what task create takes.
type TaskInput struct {
	Title       string
	Assignee    string
	Agent       string
	Due         string
	Purpose     string
	Interaction string // the origin; the purpose is follow-up unless given
	Description string
}

// CreateTask creates a task of the actor's holder in todo, or returns the
// one that already has the same interaction and purpose (WORK-INV-05).
// Data that contradicts it is DEDUP_CONFLICT.
func (ws *Workspace) CreateTask(actorID string, in TaskInput, opt ViewOptions) (*Result, error) {
	if err := tasks.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	holder := actor.Holder
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, invalidValue("title", "give --title", "a task needs a title")
	}
	assignee, agent := strings.TrimSpace(in.Assignee), strings.TrimSpace(in.Agent)
	if assignee == "" {
		return nil, invalidValue("assignee", "give --assignee <a live human of your holder>", "a task needs an assignee")
	}
	due, err := parseDate("due", in.Due)
	if err != nil {
		return nil, err
	}
	rec := &Task{Holder: holder, Title: title, Assignee: assignee, Agent: agent, Due: due,
		Purpose: strings.TrimSpace(in.Purpose)}
	if ref := strings.TrimSpace(in.Interaction); ref != "" {
		it, err := loadLinked(ws, "interaction", interactions, ref)
		if err != nil {
			return nil, err
		}
		if it.Holder != holder {
			return nil, newError(CodeHolderMismatch, "a task can come only from an interaction of its own holder",
				"%s belongs to %s, not to %s", ref, it.Holder, holder).with("field", "interaction")
		}
		rec.From, rec.Purpose = ref, firstOf(rec.Purpose, defaultPurpose)
		// A create that repeats an earlier one finds its task before the
		// catalogs and the people are judged again: they may have changed
		// since, and the repeat must stay safe.
		ex, err := ws.taskWithOrigin(rec)
		if err != nil {
			return nil, err
		}
		if ex != nil {
			if err := sameTask(ex, rec); err != nil {
				return nil, err
			}
			return ws.finishTask(opt, false)(tasks.result(OutcomeExisting, ex, nil, opt))
		}
	}
	if rec.Category, err = ws.checkStatus(holder, initialStatus, false); err != nil {
		return nil, err
	}
	rec.Status, rec.Live = initialStatus, isLive(rec.Category)
	if rec.Purpose != "" {
		if err := ws.checkPurpose(holder, rec.Purpose); err != nil {
			return nil, err
		}
	}
	if err := ws.checkAssignment(holder, assignee, agent, true, true); err != nil {
		return nil, err
	}
	if in.Description != "" {
		rec.Text = &in.Description
	}
	tid, err := typeid.WithPrefix(strings.TrimSuffix(tasks.Prefix, "_"))
	if err != nil {
		return nil, err
	}
	rec.ID = tid.String()

	got, found, err := storage.Create(ws.Store, rec, actorID, tasks.Path, tasks.keys)
	if errors.Is(err, storage.ErrKeyConflict) {
		return nil, ws.originConflict(rec, err)
	}
	if err != nil {
		return nil, err
	}
	if found {
		ex, _, err := tasks.load(ws, got)
		if err != nil {
			return nil, err
		}
		if err := sameTask(ex, rec); err != nil {
			return nil, err
		}
		return ws.finishTask(opt, false)(tasks.result(OutcomeExisting, ex, nil, opt))
	}
	touched := touchedIf("from", rec.From != "", "purpose", rec.Purpose != "", "description", in.Description != "")
	return ws.finishTask(opt, in.Description != "")(tasks.result(OutcomeCreated, rec, touched, opt))
}

// taskWithOrigin returns the task that holds the origin of rec, or nil when
// there is none, or when an interrupted create left its key without a task.
func (ws *Workspace) taskWithOrigin(rec *Task) (*Task, error) {
	for _, k := range taskKeys(rec) {
		owner, err := ws.Store.KeyOwner(k)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		ex, _, err := tasks.load(ws, owner)
		var e *Error
		if errors.As(err, &e) && e.Code == CodeNotFound {
			continue
		}
		return ex, err
	}
	return nil, nil
}

// originConflict names the task that already has the origin of rec.
func (ws *Workspace) originConflict(rec *Task, cause error) error {
	for _, k := range taskKeys(rec) {
		if owner, err := ws.Store.KeyOwner(k); err == nil && owner != rec.ID {
			e := dedupConflict("task", owner, "interaction", "the interaction %s already has a task with the purpose %s", rec.From, rec.Purpose)
			e.Cause = cause
			return e
		}
	}
	return cause
}

// sameTask is DEDUP_CONFLICT when the data a create sent contradicts the task
// it found: another title or assignee, or an agent or due date it gave that
// differs. A create never changes a record.
func sameTask(ex, rec *Task) error {
	return sameData("task", ex.ID,
		clashing("title", ex.Title != rec.Title, rec.Title, "%s already exists with the title %q, not %q", ex.ID, ex.Title, rec.Title),
		clashing("assignee", ex.Assignee != rec.Assignee, rec.Assignee, "%s is assigned to %s, not to %s", ex.ID, ex.Assignee, rec.Assignee),
		clashing("agent", rec.Agent != "" && ex.Agent != rec.Agent, rec.Agent, "%s has the agent %q, not %q", ex.ID, ex.Agent, rec.Agent),
		clashing("due", rec.Due != "" && ex.Due != rec.Due, rec.Due, "%s is due %q, not %q", ex.ID, ex.Due, rec.Due))
}

// TaskChange is what task update takes; nil leaves a field as it is. The
// status, the origin and the purpose never change here.
type TaskChange struct {
	Title       *string
	Assignee    *string
	Agent       *string // "" clears it
	Due         *string // "" clears it
	Description *string // "" clears it
	// Status exists only to be refused: a status changes with its transition.
	Status *string
}

// UpdateTask changes a task of the actor's holder; another holder's is
// NOT_FOUND (WORKSPACE-INV-02). The status changes only through
// SetTaskStatus (WORK-INV-07). A task whose status left the catalog must
// get a new one first.
func (ws *Workspace) UpdateTask(actorID, id string, ch TaskChange, opt ViewOptions) (*Result, error) {
	if err := tasks.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := tasks.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	if ch.Status != nil {
		return nil, invalidValue("status", "run kravis task status <task> <status>", "update cannot change the status: it changes only through task status").
			with("value", *ch.Status)
	}
	var due string
	if ch.Due != nil {
		if due, err = parseDate("due", *ch.Due); err != nil {
			return nil, err
		}
	}
	touched := touchedIf("title", ch.Title != nil, "assignee", ch.Assignee != nil, "agent", ch.Agent != nil,
		"due", ch.Due != nil, "description", ch.Description != nil)
	rec, changed, err := tasks.updateIn(ws, actor.Holder, actorID, id, func(r *Task) error {
		cat, err := ws.checkStatus(r.Holder, r.Status, true)
		if err != nil {
			return err
		}
		// Category and live are computed on every write, so a catalog that
		// moved the status shows up in the record at the next edit.
		wasLive := r.Live
		r.Category, r.Live = cat, isLive(cat)
		prevAssignee, prevAgent := r.Assignee, r.Agent
		if ch.Title != nil {
			r.Title = strings.TrimSpace(*ch.Title)
		}
		if ch.Assignee != nil {
			r.Assignee = strings.TrimSpace(*ch.Assignee)
		}
		if ch.Agent != nil {
			r.Agent = strings.TrimSpace(*ch.Agent)
		}
		if ch.Due != nil {
			r.Due = due
		}
		reopened := r.Live && !wasLive
		if err := ws.checkAssignment(r.Holder, r.Assignee, r.Agent, reopened || r.Assignee != prevAssignee, reopened && r.Agent != "" || r.Agent != prevAgent); err != nil {
			return err
		}
		if ch.Description != nil {
			cur, err := ws.Store.Text(tasks.Path(id))
			if err != nil {
				return err
			}
			if cur != *ch.Description {
				r.Text = ch.Description
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ws.finishTask(opt, ch.Description != nil)(tasks.result(outcome(changed), rec, touched, opt))
}

// SetTaskStatus moves a task of the actor's holder to a status of the
// catalog, with its category and live flag (WORK-INV-07, WORKSPACE-INV-06).
// Moving it to the status it has changes nothing.
func (ws *Workspace) SetTaskStatus(actorID, id, status string, opt ViewOptions) (*Result, error) {
	if err := tasks.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := tasks.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	status = strings.TrimSpace(status)
	cat, err := ws.checkStatus(actor.Holder, status, false)
	if err != nil {
		return nil, err
	}
	rec, changed, err := tasks.updateIn(ws, actor.Holder, actorID, id, func(r *Task) error {
		// A task that becomes live again needs a live assignee and agent, as
		// a new one does (WORK-INV-03, WORK-INV-04).
		if isLive(cat) && !r.Live {
			if err := ws.checkAssignment(r.Holder, r.Assignee, r.Agent, true, r.Agent != ""); err != nil {
				return err
			}
		}
		r.Status, r.Category, r.Live = status, cat, isLive(cat)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ws.finishTask(opt, false)(tasks.result(outcome(changed), rec, []string{"status", "category"}, opt))
}

// DeleteTask deletes a task of the actor's holder that no record refers to;
// another holder's is NOT_FOUND. Its sidecar and its origin key go with it.
func (ws *Workspace) DeleteTask(actorID, id string, opt ViewOptions) (*Result, error) {
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	var text string
	if wantsText(opt, "description") {
		if _, _, err := tasks.loadIn(ws, actor.Holder, id); err != nil {
			return nil, err
		}
		if text, err = ws.Store.Text(tasks.Path(id)); err != nil {
			return nil, err
		}
	}
	res, err := tasks.removeIn(ws, actor.Holder, id, opt)
	if err != nil {
		return nil, err
	}
	res.Data = withField(res.Data, "description", text)
	return res, nil
}

// TaskFilter is what task list narrows by.
type TaskFilter struct {
	// Holder limits the list to one holder's tasks; "" is no limit.
	Holder    string
	Assignee  string
	Agent     string
	Category  string
	Overdue   bool   // live, with a due date before today in the workspace timezone
	DueBefore string // a date: only tasks due before it
}

// ListTasks returns a page of tasks by due date. Live tasks only, unless
// All or a category is given.
func (ws *Workspace) ListTasks(f TaskFilter, opt ListOptions) (*ListResult, error) {
	if f.Category != "" {
		if !slices.Contains(statusCategories, f.Category) {
			return nil, InvalidFlag("category", statusCategories, "%q is not a task status category", f.Category)
		}
		opt.All = true // asking for a category asks for it whether live or not
	}
	before, err := parseDate("due-before", f.DueBefore)
	if err != nil {
		return nil, err
	}
	var today string
	if f.Overdue {
		loc, err := ws.location()
		if err != nil {
			return nil, err
		}
		today = ws.clock().In(loc).Format(dateLayout)
	}
	keep := func(t *Task) bool {
		return (f.Assignee == "" || t.Assignee == f.Assignee) &&
			(f.Agent == "" || t.Agent == f.Agent) &&
			(f.Category == "" || t.Category == f.Category) &&
			(!f.Overdue || (t.Live && t.Due != "" && t.Due < today)) &&
			(before == "" || (t.Due != "" && t.Due < before))
	}
	print := fmt.Sprintf("holder=%s|assignee=%s|agent=%s|category=%s|overdue=%t|before=%s",
		f.Holder, f.Assignee, f.Agent, f.Category, f.Overdue, before)
	res, err := tasks.list(ws, f.Holder, opt, keep, print)
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

// GetTask returns the view of a task. A read limited to a holder finds only
// that holder's tasks.
func (ws *Workspace) GetTask(holder, id string, opt ViewOptions) (*GetResult, error) {
	v, err := tasks.getIn(ws, holder, id, opt)
	if err == nil && wantsText(opt, "description") {
		v, err = ws.addText(v, "description")
	}
	if err != nil {
		return nil, err
	}
	return &GetResult{Data: v, Filters: holderFilter(holder)}, nil
}

// wantsText says whether the options ask for a sidecar field: --full or
// --fields with its name.
func wantsText(opt ViewOptions, field string) bool {
	return opt.Full || slices.Contains(opt.Fields, field)
}

// withField adds a text field to a view, when there is text.
func withField(v View, name, text string) View {
	if text == "" {
		return v
	}
	raw, _ := json.Marshal(text)
	return append(v, storage.Field{Name: name, Value: raw})
}

// addText reads the description of the task a view shows.
func (ws *Workspace) addText(v View, field string) (View, error) {
	for _, f := range v {
		if f.Name == "id" {
			var id string
			if json.Unmarshal(f.Value, &id) != nil || !tasks.validID(id) {
				break
			}
			text, err := ws.Store.Text(tasks.Path(id))
			return withField(v, field, text), err
		}
	}
	return v, nil
}

// finishTask adds the description to a write's result when the options ask
// for it or the write touched it.
func (ws *Workspace) finishTask(opt ViewOptions, touched bool) func(*Result, error) (*Result, error) {
	return func(res *Result, err error) (*Result, error) {
		if err != nil || (!touched && !wantsText(opt, "description")) {
			return res, err
		}
		res.Data, err = ws.addText(res.Data, "description")
		return res, err
	}
}

// tasksReferringTo finds the tasks that have id as their holder, assignee,
// agent or origin.
func tasksReferringTo(ws *Workspace, id string) ([]string, error) {
	return scanTasks(ws, func(t *Task) bool {
		return t.Holder == id || t.Assignee == id || t.Agent == id || t.From == id
	})
}

// tasksOf finds the tasks whose holder is id.
func tasksOf(ws *Workspace, id string) ([]string, error) {
	return scanTasks(ws, func(t *Task) bool { return t.Holder == id })
}

// liveTasksOf finds the live tasks that user is the assignee or agent of.
func (ws *Workspace) liveTasksOf(user string) ([]string, error) {
	return scanTasks(ws, func(t *Task) bool { return t.Live && (t.Assignee == user || t.Agent == user) })
}

func scanTasks(ws *Workspace, match func(*Task) bool) ([]string, error) {
	rows, err := tasks.scan(ws)
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

// auditTaskLinks reports what tasks refer to: records that do not exist, a
// holder that is not an own organization, people and interactions of another
// holder, an agent that the assignee does not handle, and a status or
// purpose the catalog no longer has or whose category or live flag the
// catalog contradicts.
func auditTaskLinks(st *storage.Store, add addFinding) error {
	ts, err := loadForCheck(st, tasks)
	if err != nil || len(ts) == 0 {
		return err
	}
	us, err := loadForCheck(st, users)
	if err != nil {
		return err
	}
	orgs, err := loadForCheck(st, organizations)
	if err != nil {
		return err
	}
	its, err := loadForCheck(st, interactions)
	if err != nil {
		return err
	}
	var cfg *Config // nil when config.json cannot be read: check reports that
	if data, _, err := st.Raw(ConfigPath); err == nil {
		c := Config{}
		if json.Unmarshal(data, &c) == nil && c.Validate() == nil {
			cfg = &c
		}
	}
	for _, id := range slices.Sorted(maps.Keys(ts)) {
		t := ts[id]
		switch o, ok := orgs[t.rec.Holder]; {
		case !hasID("org_", t.rec.Holder):
		case !ok:
			add(FindingDanglingReference, t.path, "the holder %s does not exist", t.rec.Holder)
		case !o.rec.Own:
			add(FindingInvalidRecord, t.path, "the holder %s is not an own organization", t.rec.Holder)
		}
		assignee, hasAssignee := us[t.rec.Assignee]
		if hasID("us_", t.rec.Assignee) {
			switch {
			case !hasAssignee:
				add(FindingDanglingReference, t.path, "the assignee %s does not exist", t.rec.Assignee)
			case assignee.rec.Kind != KindHuman:
				add(FindingInvalidRecord, t.path, "the assignee %s is an agent, and a task is assigned to a human", t.rec.Assignee)
			case assignee.rec.Holder != t.rec.Holder:
				add(FindingInvalidRecord, t.path, "the assignee %s works for %s, not for the holder %s", t.rec.Assignee, assignee.rec.Holder, t.rec.Holder)
			case t.rec.Live && !assignee.rec.Live:
				add(FindingInvalidRecord, t.path, "the assignee %s is not live, and the task is", t.rec.Assignee)
			}
		}
		if hasID("us_", t.rec.Agent) {
			switch a, ok := us[t.rec.Agent]; {
			case !ok:
				add(FindingDanglingReference, t.path, "the agent %s does not exist", t.rec.Agent)
			case a.rec.Kind != KindAgent:
				add(FindingInvalidRecord, t.path, "the agent %s is a human, and the assigned agent is an agent", t.rec.Agent)
			case a.rec.Holder != t.rec.Holder:
				add(FindingInvalidRecord, t.path, "the agent %s works for %s, not for the holder %s", t.rec.Agent, a.rec.Holder, t.rec.Holder)
			case a.rec.Handler != t.rec.Assignee:
				add(FindingInvalidRecord, t.path, "the agent %s is handled by %s, not by the assignee %s", t.rec.Agent, a.rec.Handler, t.rec.Assignee)
			case t.rec.Live && !a.rec.Live:
				add(FindingInvalidRecord, t.path, "the agent %s is not live, and the task is", t.rec.Agent)
			}
		}
		if hasID("it_", t.rec.From) {
			switch it, ok := its[t.rec.From]; {
			case !ok:
				add(FindingDanglingReference, t.path, "the origin %s does not exist", t.rec.From)
			case it.rec.Holder != t.rec.Holder:
				add(FindingInvalidRecord, t.path, "the origin %s belongs to %s, not to the holder %s", t.rec.From, it.rec.Holder, t.rec.Holder)
			}
		}
		if cfg == nil {
			continue
		}
		conf := cfg.forHolder(t.rec.Holder)
		if slugID.MatchString(t.rec.Status) {
			i := slices.IndexFunc(conf.Catalogs.TaskStatuses, func(s Status) bool { return s.Name == t.rec.Status })
			switch {
			case i < 0:
				add(FindingValueNotInCatalog, t.path, "the status %q is not in the task status catalog for holder %s; the task must get another status",
					t.rec.Status, t.rec.Holder)
			case conf.Catalogs.TaskStatuses[i].Category != t.rec.Category:
				add(FindingInvalidRecord, t.path, "the category %q disagrees with the catalog, which puts %q in %q",
					t.rec.Category, t.rec.Status, conf.Catalogs.TaskStatuses[i].Category)
			case isLive(t.rec.Category) != t.rec.Live:
				add(FindingInvalidRecord, t.path, "live is %t, but the catalog puts %q in %q", t.rec.Live, t.rec.Status, t.rec.Category)
			}
		}
		if slugID.MatchString(t.rec.Purpose) && !slices.Contains(conf.Catalogs.Purposes, t.rec.Purpose) {
			add(FindingValueNotInCatalog, t.path, "the purpose %q is not in the purpose catalog for holder %s", t.rec.Purpose, t.rec.Holder)
		}
	}
	return nil
}
