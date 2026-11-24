package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/diegomarino/kravis/internal/storage"
)

// Kinds of natural keys of the directory.
const (
	KeyPersonEmail        = "person-email"
	KeyOrganizationDomain = "organization-domain"
)

// genericLocalParts are local parts of addresses that belong to a role, not
// a person (GENERIC_ADDRESS).
var genericLocalParts = []string{"info", "hello", "contact", "noreply", "no-reply", "support", "sales", "admin", "team", "office"}

// providerDomains are email providers: never an organization's domain.
var providerDomains = []string{
	"gmail.com", "googlemail.com", "outlook.com", "hotmail.com", "live.com", "yahoo.com",
	"icloud.com", "me.com", "proton.me", "protonmail.com", "gmx.com",
}

var (
	emailForm  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	domainForm = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
)

// NormalEmail is how emails are compared: trimmed and lowercased as a whole.
func NormalEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// NormalDomain is how domains are compared: lowercased, without a leading
// www.
func NormalDomain(s string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "www.")
}

func emailProblem(e string) string {
	if !emailForm.MatchString(e) || e != NormalEmail(e) {
		return fmt.Sprintf("%q is not an email address", e)
	}
	return ""
}

func domainProblem(d string) string {
	switch {
	case !domainForm.MatchString(d) || d != NormalDomain(d):
		return fmt.Sprintf("%q is not a domain such as example.com", d)
	case slices.Contains(providerDomains, d):
		return fmt.Sprintf("%q is an email provider, never an organization's domain", d)
	}
	return ""
}

// normalList normalizes values, refuses invalid ones and returns them sorted
// without repeats.
func normalList(field string, values []string, normal func(string) string, problem func(string) string) ([]string, error) {
	var out []string
	for _, v := range values {
		v = normal(v)
		if p := problem(v); p != "" {
			hint := ""
			if field == "domain" && slices.Contains(providerDomains, v) {
				hint = "a provider's domain is shared by many organizations; give the organization's own domain, or none"
			}
			return nil, invalidValue(field, hint, "%s", p).with("value", v)
		}
		out = append(out, v)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// listRules reports invalid or repeated values of a stored list.
func listRules(name string, values []string, problem func(string) string) []string {
	var out []string
	for i, v := range values {
		if p := problem(v); p != "" {
			out = append(out, p)
		}
		if slices.Contains(values[:i], v) {
			out = append(out, fmt.Sprintf("%s %q is repeated", name, v))
		}
	}
	return out
}

var people = newKind(kind[Person, *Person]{
	Type: Type{Name: "person", Dir: "people", Prefix: "pe_",
		Order: "name compared as its slug, else the smallest email, else the ID; then the ID"},
	compact: []compactField{{"id", ""}, {"name", ""}, {"live", "false"}, {"emails", "[]"}},
	sortKey: func(p *Person) string {
		if len(p.Emails) > 0 {
			return firstOf(p.Name, slices.Min(p.Emails), p.ID)
		}
		return firstOf(p.Name, p.ID)
	},
	live:     func(p *Person) bool { return p.Live },
	keyKinds: []string{KeyPersonEmail},
	keys: func(p *Person) []storage.Key {
		var keys []storage.Key
		for _, e := range p.Emails {
			keys = append(keys, storage.Key{Kind: KeyPersonEmail, Value: e})
		}
		return keys
	},
	rules: func(p *Person) []string {
		var out []string
		if strings.TrimSpace(p.Name) == "" && len(p.Emails) == 0 {
			out = append(out, "a person needs a name, an email or both")
		}
		out = append(out, listRules("email", p.Emails, emailProblem)...)
		return append(out, affiliationRules(p)...)
	},
})

var organizations = newKind(kind[Organization, *Organization]{
	Type: Type{Name: "organization", Dir: "organizations", Prefix: "org_",
		Order: "name compared as its slug; then the ID"},
	compact:  []compactField{{"id", ""}, {"name", ""}, {"live", "false"}, {"domains", "[]"}, {"own", "false"}},
	sortKey:  func(o *Organization) string { return o.Name },
	live:     func(o *Organization) bool { return o.Live },
	keyKinds: []string{KeyOrganizationDomain},
	keys: func(o *Organization) []storage.Key {
		var keys []storage.Key
		for _, d := range o.Domains {
			keys = append(keys, storage.Key{Kind: KeyOrganizationDomain, Value: d})
		}
		return keys
	},
	rules: func(o *Organization) []string {
		var out []string
		if strings.TrimSpace(o.Name) == "" {
			out = append(out, "an organization needs a name")
		}
		return append(out, listRules("domain", o.Domains, domainProblem)...)
	},
})

// sameName says whether two names are the same name: equal, or with the
// same slug.
func sameName(a, b string) bool { return a == b || (Slug(a) != "" && Slug(a) == Slug(b)) }

// personSlug is the slug of a new person's ID: from the name if it has one,
// else the local part of the first email plus the first label of its
// domain.
func personSlug(name string, emails []string) string {
	if s := Slug(name); s != "" || len(emails) == 0 {
		return s
	}
	local, domain, _ := strings.Cut(emails[0], "@")
	label, _, _ := strings.Cut(domain, ".")
	return strings.Trim(Slug(local)+"-"+Slug(label), "-")
}

func dedupConflict(name, id, field, format string, args ...any) *Error {
	return newError(CodeDedupConflict, fmt.Sprintf("kravis %s get %s shows it; change it with kravis %s update", name, id, name),
		format, args...).with("id", id).with("field", field)
}

// clash is one way in which the data of a create can contradict the record
// it found: field says where, when says whether it does, value is what was
// given.
type clash struct {
	field  string
	when   bool
	value  any
	format string
	args   []any
}

func clashing(field string, when bool, value any, format string, args ...any) clash {
	return clash{field, when, value, format, args}
}

// sameData is nil when the data a create sent does not contradict the
// record id it found, and otherwise DEDUP_CONFLICT for the first clash.
func sameData(name, id string, clashes ...clash) error {
	for _, c := range clashes {
		if c.when {
			e := dedupConflict(name, id, c.field, c.format, c.args...)
			if c.value != nil {
				_ = e.with("value", c.value)
			}
			return e
		}
	}
	return nil
}

// PersonInput is what person create takes.
type PersonInput struct {
	Name   string   `json:"name,omitempty" jsonschema:"pattern=\\S,description=a person needs a name and or an email" jsonschema_extras:"x-cli=--name"`
	Emails []string `json:"emails,omitempty" jsonschema:"description=lowercase addresses" jsonschema_extras:"x-cli=--email (repeatable)"`
}

// CreatePerson creates a person, or returns the person that already has one
// of the emails (DIRECTORY-INV-02). Data that contradicts that person is
// DEDUP_CONFLICT; a value the person lacks counts as a contradiction, since
// a create never changes a record.
func (ws *Workspace) CreatePerson(actor string, in PersonInput, opt ViewOptions) (*Result, error) {
	if err := people.checkFields(opt); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	emails, err := normalList("email", in.Emails, NormalEmail, emailProblem)
	if err != nil {
		return nil, err
	}
	if name == "" && len(emails) == 0 {
		return nil, invalidValue("name", "give --name, --email or both", "a person needs a name, an email or both")
	}
	base := personSlug(name, emails)
	if base == "" {
		return nil, invalidValue("name", "give an --email too, so the ID can come from it",
			"%q has no letter or digit with an ASCII form for the ID", name)
	}
	touched := touchedIf("name", name != "", "emails", len(emails) > 0)
	rec := &Person{Name: name, Emails: emails, Live: true}
	ex, similar, err := people.create(ws, actor, rec, base)
	if err != nil {
		return nil, err
	}
	if ex != nil {
		clashes := []clash{clashing("name", name != "" && !sameName(ex.Name, name), name,
			"an email given belongs to %s, whose name is %q, not %q", ex.ID, ex.Name, name)}
		for _, e := range emails {
			clashes = append(clashes, clashing("email", !slices.Contains(ex.Emails, e), e,
				"an email given belongs to %s, which does not have %q", ex.ID, e))
		}
		if err := sameData("person", ex.ID, clashes...); err != nil {
			return nil, err
		}
		return people.result(OutcomeExisting, ex, nil, opt)
	}
	var warnings []Warning
	if similar != "" {
		warnings = append(warnings, similarExists("person", rec.ID, similar))
	}
	for _, e := range emails {
		if local, _, _ := strings.Cut(e, "@"); slices.Contains(genericLocalParts, local) {
			warnings = append(warnings, Warning{Code: WarnGenericAddress,
				Message: fmt.Sprintf("%s is a generic address, which usually belongs to a role, not a person", e),
				Details: map[string]any{"email": e}})
		}
	}
	return people.result(OutcomeCreated, rec, touched, opt, warnings...)
}

func similarExists(name, id, existing string) Warning {
	return Warning{Code: WarnSimilarExists,
		Message: fmt.Sprintf("%s %s already exists, so this %s got the ID %s", name, existing, name, id),
		Details: map[string]any{"id": existing}}
}

// ResolvePerson turns a person ID or one of their emails into the ID.
func (ws *Workspace) ResolvePerson(ref string) (string, error) {
	if !strings.Contains(ref, "@") {
		return ref, nil
	}
	id, err := ws.Store.KeyOwner(storage.Key{Kind: KeyPersonEmail, Value: NormalEmail(ref)})
	if errors.Is(err, storage.ErrNotFound) {
		return "", people.notFound(ref)
	}
	return id, err
}

// PersonChange is what person update takes; nil leaves a field as it is.
type PersonChange struct {
	Name         *string
	Emails       []string
	RemoveEmails []string
	Live         *bool
}

// UpdatePerson changes a person. Emails it adds get their key first: one
// that belongs to another person is DEDUP_CONFLICT and nothing is written.
func (ws *Workspace) UpdatePerson(actor, ref string, ch PersonChange, opt ViewOptions) (*Result, error) {
	if err := people.checkFields(opt); err != nil {
		return nil, err
	}
	id, err := ws.ResolvePerson(ref)
	if err != nil {
		return nil, err
	}
	add, err := normalList("email", ch.Emails, NormalEmail, emailProblem)
	if err != nil {
		return nil, err
	}
	drop, err := normalList("remove-email", ch.RemoveEmails, NormalEmail, emailProblem)
	if err != nil {
		return nil, err
	}
	touched := touchedIf("name", ch.Name != nil, "emails", len(add)+len(drop) > 0, "live", ch.Live != nil)
	rec, changed, err := people.update(ws, actor, id, func(p *Person) error {
		if ch.Name != nil {
			p.Name = strings.TrimSpace(*ch.Name)
		}
		p.Emails = slices.Compact(slices.Sorted(slices.Values(slices.Concat(p.Emails, add))))
		p.Emails = slices.DeleteFunc(p.Emails, func(e string) bool { return slices.Contains(drop, e) })
		if ch.Live != nil {
			p.Live = *ch.Live
		}
		return nil
	})
	if errors.Is(err, storage.ErrKeyConflict) {
		return nil, ws.keyConflict("person", KeyPersonEmail, "email", id, add, err)
	}
	if err != nil {
		return nil, err
	}
	return people.result(outcome(changed), rec, touched, opt)
}

// keyConflict names the value of values whose key belongs to a record
// other than id.
func (ws *Workspace) keyConflict(name, kind, field, id string, values []string, cause error) error {
	for _, v := range values {
		owner, err := ws.Store.KeyOwner(storage.Key{Kind: kind, Value: v})
		if err == nil && owner != id {
			e := dedupConflict(name, owner, field, "%s %q already belongs to %s", field, v, owner).with("value", v)
			e.Cause = cause
			return e
		}
	}
	return cause
}

func outcome(changed bool) string {
	if changed {
		return OutcomeUpdated
	}
	return OutcomeUnchanged
}

// GetPerson returns the view of a person, by ID or email.
func (ws *Workspace) GetPerson(ref string, opt ViewOptions) (View, error) {
	id, err := ws.ResolvePerson(ref)
	if err != nil {
		return nil, err
	}
	return people.get(ws, id, opt)
}

// ListPeople returns a page of people, by name.
func (ws *Workspace) ListPeople(opt ListOptions) (*ListResult, error) {
	return people.list(ws, "", opt, nil, "")
}

// DeletePerson deletes a person, by ID or email, with their email keys.
func (ws *Workspace) DeletePerson(ref string, opt ViewOptions) (*Result, error) {
	id, err := ws.ResolvePerson(ref)
	if err != nil {
		return nil, err
	}
	return people.remove(ws, id, opt)
}

// OrganizationInput is what organization create takes.
type OrganizationInput struct {
	Name    string   `json:"name" jsonschema:"pattern=\\S" jsonschema_extras:"x-cli=--name"`
	Own     bool     `json:"own,omitempty" jsonschema:"description=marks an own organization: a holder" jsonschema_extras:"x-cli=--own"`
	Domains []string `json:"domains,omitempty" jsonschema:"description=lowercase and without www. and never an email provider domain" jsonschema_extras:"x-cli=--domain (repeatable)"`
}

// CreateOrganization creates an organization, or returns the one that
// already has one of the domains (DIRECTORY-INV-02). Data that contradicts
// it is DEDUP_CONFLICT.
func (ws *Workspace) CreateOrganization(actor string, in OrganizationInput, opt ViewOptions) (*Result, error) {
	if err := organizations.checkFields(opt); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if Slug(name) == "" {
		return nil, invalidValue("name", "the name needs at least one letter or digit that has an ASCII form",
			"%q is not a usable organization name", name)
	}
	domains, err := normalList("domain", in.Domains, NormalDomain, domainProblem)
	if err != nil {
		return nil, err
	}
	touched := touchedIf("name", true, "domains", len(domains) > 0, "own", in.Own)
	rec := &Organization{Name: name, Domains: domains, Own: in.Own, Live: true}
	ex, similar, err := organizations.create(ws, actor, rec, Slug(name))
	if err != nil {
		return nil, err
	}
	if ex != nil {
		clashes := []clash{
			clashing("name", !sameName(ex.Name, name), name, "a domain given belongs to %s, whose name is %q, not %q", ex.ID, ex.Name, name),
			clashing("own", in.Own && !ex.Own, nil, "a domain given belongs to %s, which is not an own organization", ex.ID),
		}
		for _, d := range domains {
			clashes = append(clashes, clashing("domain", !slices.Contains(ex.Domains, d), d,
				"a domain given belongs to %s, which does not have %q", ex.ID, d))
		}
		if err := sameData("organization", ex.ID, clashes...); err != nil {
			return nil, err
		}
		return organizations.result(OutcomeExisting, ex, nil, opt)
	}
	var warnings []Warning
	if similar != "" {
		warnings = append(warnings, similarExists("organization", rec.ID, similar))
	}
	return organizations.result(OutcomeCreated, rec, touched, opt, warnings...)
}

// OrganizationChange is what organization update takes; nil leaves a field
// as it is.
type OrganizationChange struct {
	Name          *string
	Own           *bool
	Domains       []string
	RemoveDomains []string
	Live          *bool
}

// UpdateOrganization changes an organization. The own mark cannot be
// removed while the organization is a holder (REFERENCED).
func (ws *Workspace) UpdateOrganization(actor, id string, ch OrganizationChange, opt ViewOptions) (*Result, error) {
	if err := organizations.checkFields(opt); err != nil {
		return nil, err
	}
	add, err := normalList("domain", ch.Domains, NormalDomain, domainProblem)
	if err != nil {
		return nil, err
	}
	drop, err := normalList("remove-domain", ch.RemoveDomains, NormalDomain, func(string) string { return "" })
	if err != nil {
		return nil, err
	}
	if ch.Name != nil && strings.TrimSpace(*ch.Name) == "" {
		return nil, invalidValue("name", "", "an organization needs a name")
	}
	if ch.Own != nil && !*ch.Own {
		holders, err := holdersOf(ws, id)
		if err != nil {
			return nil, err
		}
		if len(holders) > 0 {
			return nil, newError(CodeReferenced, "users and work records listed in details.referrers have it as their holder",
				"%s is a holder, so it stays an own organization", id).with("referrers", holders)
		}
	}
	touched := touchedIf("name", ch.Name != nil, "domains", len(add)+len(drop) > 0, "own", ch.Own != nil, "live", ch.Live != nil)
	rec, changed, err := organizations.update(ws, actor, id, func(o *Organization) error {
		if ch.Name != nil {
			o.Name = strings.TrimSpace(*ch.Name)
		}
		if ch.Own != nil {
			o.Own = *ch.Own
		}
		o.Domains = slices.Compact(slices.Sorted(slices.Values(slices.Concat(o.Domains, add))))
		o.Domains = slices.DeleteFunc(o.Domains, func(d string) bool { return slices.Contains(drop, d) })
		if ch.Live != nil {
			o.Live = *ch.Live
		}
		return nil
	})
	if errors.Is(err, storage.ErrKeyConflict) {
		return nil, ws.keyConflict("organization", KeyOrganizationDomain, "domain", id, add, err)
	}
	if err != nil {
		return nil, err
	}
	return organizations.result(outcome(changed), rec, touched, opt)
}

// GetOrganization returns the view of an organization.
func (ws *Workspace) GetOrganization(id string, opt ViewOptions) (View, error) {
	return organizations.get(ws, id, opt)
}

// ListOrganizations returns a page of organizations, by name.
func (ws *Workspace) ListOrganizations(opt ListOptions) (*ListResult, error) {
	return organizations.list(ws, "", opt, nil, "")
}

// DeleteOrganization deletes an organization that nothing refers to.
func (ws *Workspace) DeleteOrganization(id string, opt ViewOptions) (*Result, error) {
	return organizations.remove(ws, id, opt)
}
