package domain

import (
	"errors"
	"slices"
	"strings"

	"github.com/diegomarino/kravis/internal/storage"
)

// userRules reports what is wrong inside a user record, whatever else
// exists (IDENTITY-INV-01..03).
func userRules(u *User) []string {
	var out []string
	if u.Kind != KindHuman && u.Kind != KindAgent {
		out = append(out, "the kind "+quote(u.Kind)+" is not human or agent")
	}
	if !hasID("org_", u.Holder) {
		out = append(out, "the holder "+quote(u.Holder)+" is not an organization ID (org_<slug>)")
	}
	switch {
	case u.Kind == KindAgent && !hasID("us_", u.Handler):
		out = append(out, "an agent needs a handler: the ID of a human user (us_<key>)")
	case u.Kind == KindHuman && u.Handler != "":
		out = append(out, "a human has no handler")
	}
	switch {
	case u.Person != "" && u.Kind == KindAgent:
		out = append(out, "only a human can be linked to a person")
	case u.Person != "" && !hasID("pe_", u.Person):
		out = append(out, "the person "+quote(u.Person)+" is not a person ID (pe_<slug>)")
	}
	return out
}

// hasID says whether id has the form of an ID with the given prefix. The
// kinds' own validID cannot be used in rules, which the kinds depend on.
func hasID(prefix, id string) bool {
	rest, ok := strings.CutPrefix(id, prefix)
	return ok && slugID.MatchString(rest)
}

func quote(s string) string { return `"` + s + `"` }

// loadLinked loads the record that a field of the input names; if it does
// not exist, NOT_FOUND says which field.
func loadLinked[T any, P interface {
	*T
	storage.Record
}](ws *Workspace, field string, k *kind[T, P], id string) (P, error) {
	rec, _, err := k.load(ws, id)
	return rec, notFoundIn(field, err)
}

// notFoundIn names the input field in a NOT_FOUND; other errors pass.
func notFoundIn(field string, err error) error {
	var e *Error
	if errors.As(err, &e) && e.Code == CodeNotFound {
		return e.with("field", field)
	}
	return err
}

// UserInput is what user create takes.
type UserInput struct {
	Key     string `json:"key" jsonschema:"pattern=^[a-z0-9]+(-[a-z0-9]+)*$,description=lowercase words joined by hyphens: the ID is us_<key>" jsonschema_extras:"x-cli=<key> (first argument)"`
	Kind    string `json:"kind" jsonschema:"description=an agent needs a handler" jsonschema_extras:"x-cli=--human|--agent"`
	Holder  string `json:"holder" jsonschema:"pattern=^org_[a-z0-9]+(-[a-z0-9]+)*$,description=an own organization: it never changes" jsonschema_extras:"x-cli=--holder"`
	Handler string `json:"handler,omitempty" jsonschema:"pattern=^us_[a-z0-9]+(-[a-z0-9]+)*$,description=a live human of the same holder: only for an agent" jsonschema_extras:"x-cli=--handler"`
	Person  string `json:"person,omitempty" jsonschema:"description=a person ID or email: only for a human" jsonschema_extras:"x-cli=--person"` // an ID or one of the person's emails
	Name    string `json:"name,omitempty" jsonschema_extras:"x-cli=--name"`
}

// checkHandler refuses a handler that is not a live human of the holder
// (IDENTITY-INV-03).
func (ws *Workspace) checkHandler(handler, holder string) error {
	h, err := loadLinked(ws, "handler", users, handler)
	if err != nil {
		return err
	}
	switch {
	case h.Kind != KindHuman:
		return invalidValue("handler", "an agent's handler is a human user", "%s is an agent, and a handler is a human", handler)
	case h.Holder != holder:
		return newError(CodeHolderMismatch, "choose a human of the agent's holder", "%s works for %s, not for %s", handler, h.Holder, holder).
			with("field", "handler")
	case !h.Live:
		return invalidValue("handler", "reactivate the handler first, or choose a live human", "%s is not live", handler)
	}
	return nil
}

// resolveLinkedPerson turns a person ID or email into the ID of an existing
// person.
func (ws *Workspace) resolveLinkedPerson(ref string) (string, error) {
	id, err := ws.ResolvePerson(ref)
	if err != nil {
		return "", people.notFound(ref).with("field", "person")
	}
	if _, err := loadLinked(ws, "person", people, id); err != nil {
		return "", err
	}
	return id, nil
}

// CreateUser creates the user us_<key>, or returns it as existing when it
// already has the same data. A user with that key and other data is
// DEDUP_CONFLICT.
func (ws *Workspace) CreateUser(actor string, in UserInput, opt ViewOptions) (*Result, error) {
	if err := users.checkFields(opt); err != nil {
		return nil, err
	}
	if !userKey.MatchString(in.Key) {
		return nil, invalidValue("key", "use lowercase letters, digits and hyphens, such as diego-petalo", "%q is not a valid user key", in.Key)
	}
	rec := &User{Holder: in.Holder, Kind: in.Kind, Handler: in.Handler, Name: strings.TrimSpace(in.Name), Live: true}
	rec.ID = users.Prefix + in.Key
	if in.Kind != KindHuman && in.Kind != KindAgent {
		return nil, InvalidFlag("kind", []string{KindHuman, KindAgent}, "a user is a human or an agent: give --human or --agent")
	}
	if in.Kind == KindHuman && in.Handler != "" {
		return nil, invalidValue("handler", "only an agent has a handler", "a human has no handler")
	}
	if in.Kind == KindAgent && in.Handler == "" {
		return nil, invalidValue("handler", "give --handler <human user of the holder>", "an agent needs a handler")
	}
	if in.Kind == KindAgent && in.Person != "" {
		return nil, invalidValue("person", "only a human can be linked to a person", "an agent cannot be linked to a person")
	}
	var err error
	if in.Person != "" {
		if rec.Person, err = ws.resolveLinkedPerson(in.Person); err != nil {
			return nil, err
		}
	}
	// A user that exists is returned before the handler and holder are
	// judged again: repeating a create stays safe after they were deactivated.
	if ex, _, err := users.load(ws, rec.ID); err == nil {
		return ws.sameUser(ex, rec, opt)
	}
	org, err := loadLinked(ws, "holder", organizations, in.Holder)
	if err != nil {
		return nil, err
	}
	if !org.Own {
		return nil, invalidValue("holder", "mark it first: kravis organization update "+in.Holder+" --own",
			"%s is not an own organization, so it cannot be a holder", in.Holder)
	}
	if in.Kind == KindAgent {
		if err := ws.checkHandler(in.Handler, in.Holder); err != nil {
			return nil, err
		}
	}
	touched := touchedIf("holder", true, "kind", true, "handler", rec.Handler != "", "person", rec.Person != "", "name", rec.Name != "")

	_, _, err = storage.Create(ws.Store, rec, actor, users.Path, nil)
	if errors.Is(err, storage.ErrExists) { // another create took the key first
		ex, _, lerr := users.load(ws, rec.ID)
		if lerr != nil {
			return nil, lerr
		}
		return ws.sameUser(ex, rec, opt)
	}
	if err != nil {
		return nil, err
	}
	return users.result(OutcomeCreated, rec, touched, opt)
}

// sameUser is the result of a create for a user that exists: existing when
// the data matches, DEDUP_CONFLICT when it contradicts.
func (ws *Workspace) sameUser(ex, rec *User, opt ViewOptions) (*Result, error) {
	var clashes []clash
	for _, d := range []struct{ field, have, want string }{
		{"holder", ex.Holder, rec.Holder}, {"kind", ex.Kind, rec.Kind},
		{"handler", ex.Handler, rec.Handler}, {"person", ex.Person, rec.Person},
	} {
		clashes = append(clashes, clashing(d.field, d.have != d.want, d.want, "%s already exists with %s %q, not %q", ex.ID, d.field, d.have, d.want))
	}
	clashes = append(clashes, clashing("name", rec.Name != "" && ex.Name != rec.Name, rec.Name,
		"%s already exists with the name %q, not %q", ex.ID, ex.Name, rec.Name))
	if err := sameData("user", ex.ID, clashes...); err != nil {
		return nil, err
	}
	return users.result(OutcomeExisting, ex, nil, opt)
}

// UserChange is what user update takes; nil leaves a field as it is. Holder,
// Kind and Handler exist only to be refused: they never change.
type UserChange struct {
	Name    *string
	Live    *bool
	Person  *string
	Holder  *string
	Kind    *string
	Handler *string
}

// UpdateUser changes a user's name, link to a person or live flag.
func (ws *Workspace) UpdateUser(actor, id string, ch UserChange, opt ViewOptions) (*Result, error) {
	if err := users.checkFields(opt); err != nil {
		return nil, err
	}
	person := ""
	if ch.Person != nil && *ch.Person != "" {
		var err error
		if person, err = ws.resolveLinkedPerson(*ch.Person); err != nil {
			return nil, err
		}
	}
	if ch.Live != nil && !*ch.Live {
		agents, err := ws.liveAgentsOf(id)
		if err != nil {
			return nil, err
		}
		if len(agents) > 0 {
			return nil, newError(CodeReferenced, "deactivate the agents in details.referrers first",
				"%s is the handler of live agents, so it stays live", id).with("referrers", agents)
		}
	}
	if ch.Live != nil && !*ch.Live {
		live, err := ws.liveTasksOf(id)
		if err != nil {
			return nil, err
		}
		if len(live) > 0 {
			return nil, newError(CodeReferenced, "finish, cancel or reassign the tasks in details.referrers first",
				"%s is the assignee or agent of live tasks, so it stays live", id).with("referrers", live)
		}
	}
	touched := touchedIf("name", ch.Name != nil, "person", ch.Person != nil, "live", ch.Live != nil)
	rec, changed, err := users.update(ws, actor, id, func(u *User) error {
		for _, never := range []struct {
			field string
			given *string
			have  string
			why   string
		}{
			{"holder", ch.Holder, u.Holder, "a user's holder never changes"},
			{"kind", ch.Kind, u.Kind, "a user is a human or an agent for good"},
			{"handler", ch.Handler, u.Handler, "an agent's handler never changes, and a human has none"},
		} {
			if never.given != nil && *never.given != never.have {
				return invalidValue(never.field, "create another user instead", "%s", never.why).with("value", *never.given)
			}
		}
		if ch.Name != nil {
			u.Name = strings.TrimSpace(*ch.Name)
		}
		if ch.Person != nil {
			u.Person = person
		}
		if ch.Live != nil {
			if *ch.Live && !u.Live && u.Kind == KindAgent {
				if err := ws.checkHandler(u.Handler, u.Holder); err != nil {
					return err
				}
			}
			u.Live = *ch.Live
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return users.result(outcome(changed), rec, touched, opt)
}

// GetUser returns the view of a user.
func (ws *Workspace) GetUser(id string, opt ViewOptions) (View, error) { return users.get(ws, id, opt) }

// ListUsers returns a page of users, by name.
func (ws *Workspace) ListUsers(opt ListOptions) (*ListResult, error) {
	return users.list(ws, "", opt, nil, "")
}

// DeleteUser deletes a user that nothing refers to.
func (ws *Workspace) DeleteUser(id string, opt ViewOptions) (*Result, error) {
	return users.remove(ws, id, opt)
}

// liveAgentsOf lists the live agents that id handles.
func (ws *Workspace) liveAgentsOf(id string) ([]string, error) {
	rows, err := users.scan(ws)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.rec.Kind == KindAgent && r.rec.Live && r.rec.Handler == id {
			out = append(out, r.id)
		}
	}
	slices.Sort(out)
	return out, nil
}

// usersReferringTo finds the users that refer to id as their holder,
// handler or person.
func usersReferringTo(ws *Workspace, id string) ([]string, error) {
	return referringUsers(ws, id, func(u *User) bool { return u.Holder == id || u.Handler == id || u.Person == id })
}

// holdersOf finds the users and work records whose holder is id.
func holdersOf(ws *Workspace, id string) ([]string, error) {
	us, err := referringUsers(ws, id, func(u *User) bool { return u.Holder == id })
	if err != nil {
		return nil, err
	}
	its, err := interactionsOf(ws, id)
	if err != nil {
		return nil, err
	}
	ts, err := tasksOf(ws, id)
	if err != nil {
		return nil, err
	}
	ds, err := dealsOf(ws, id)
	if err != nil {
		return nil, err
	}
	ps, err := projectsOf(ws, id)
	if err != nil {
		return nil, err
	}
	ns, err := notesOf(ws, id)
	return slices.Concat(us, its, ts, ds, ps, ns), err
}

func referringUsers(ws *Workspace, _ string, refers func(*User) bool) ([]string, error) {
	rows, err := users.scan(ws)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if refers(r.rec) {
			out = append(out, r.id)
		}
	}
	return out, nil
}

// peopleAffiliatedTo finds the people with an affiliation to organization id.
func peopleAffiliatedTo(ws *Workspace, id string) ([]string, error) {
	rows, err := people.scan(ws)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if slices.ContainsFunc(r.rec.Affiliations, func(a Affiliation) bool { return a.Organization == id }) {
			out = append(out, r.id)
		}
	}
	return out, nil
}

// Affiliation is a person's link to an organization, with their title there.
type Affiliation struct {
	Organization string `json:"organization"`
	Title        string `json:"title,omitempty"`
}

// affiliationRules reports a person's affiliations that are malformed or
// repeat an organization (DIRECTORY-INV-04).
func affiliationRules(p *Person) []string {
	var out []string
	for i, a := range p.Affiliations {
		if !hasID("org_", a.Organization) {
			out = append(out, "the affiliation to "+quote(a.Organization)+" is not to an organization ID (org_<slug>)")
		}
		if slices.ContainsFunc(p.Affiliations[:i], func(b Affiliation) bool { return b.Organization == a.Organization }) {
			out = append(out, "the person has more than one affiliation to "+a.Organization)
		}
	}
	return out
}

// AddAffiliation links a person to an organization with a title, one per
// pair. On an existing pair it updates the title, if one is given; created,
// updated and unchanged say which happened.
func (ws *Workspace) AddAffiliation(actor, personRef, org string, title *string, opt ViewOptions) (*Result, error) {
	if err := people.checkFields(opt); err != nil {
		return nil, err
	}
	id, err := ws.ResolvePerson(personRef)
	if err != nil {
		return nil, err
	}
	if _, err := loadLinked(ws, "organization", organizations, org); err != nil {
		return nil, err
	}
	var existed bool
	rec, changed, err := people.update(ws, actor, id, func(p *Person) error {
		i := slices.IndexFunc(p.Affiliations, func(a Affiliation) bool { return a.Organization == org })
		existed = i >= 0
		if !existed {
			p.Affiliations = append(p.Affiliations, Affiliation{Organization: org})
			i = len(p.Affiliations) - 1
		}
		if title != nil {
			p.Affiliations[i].Title = strings.TrimSpace(*title)
		}
		slices.SortFunc(p.Affiliations, func(a, b Affiliation) int { return strings.Compare(a.Organization, b.Organization) })
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := outcome(changed)
	if changed && !existed {
		out = OutcomeCreated
	}
	return people.result(out, rec, []string{"affiliations"}, opt)
}

// RemoveAffiliation ends a person's affiliation to an organization.
func (ws *Workspace) RemoveAffiliation(actor, personRef, org string, opt ViewOptions) (*Result, error) {
	if err := people.checkFields(opt); err != nil {
		return nil, err
	}
	id, err := ws.ResolvePerson(personRef)
	if err != nil {
		return nil, err
	}
	rec, _, err := people.update(ws, actor, id, func(p *Person) error {
		before := len(p.Affiliations)
		p.Affiliations = slices.DeleteFunc(p.Affiliations, func(a Affiliation) bool { return a.Organization == org })
		if len(p.Affiliations) == before {
			return newError(CodeNotFound, "kravis person get "+id+" --fields affiliations shows them",
				"%s has no affiliation to %s", id, org).with("id", org).with("field", "organization")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return people.result(OutcomeDeleted, rec, []string{"affiliations"}, opt)
}
