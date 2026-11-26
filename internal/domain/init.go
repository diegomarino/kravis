package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.jetify.com/typeid"

	"github.com/diegomarino/kravis/internal/storage"
)

// InitInput is what `kravis init` takes.
type InitInput struct {
	Timezone     string
	Organization string
	UserKey      string
	UserName     string // optional
	UserEmail    string // optional: links the user to the person with this email
}

// Outcomes of a write.
const (
	OutcomeCreated  = "created"
	OutcomeExisting = "existing"
	OutcomeUpdated  = "updated"
)

// InitResult is what init created, or found.
type InitResult struct {
	Outcome      string `json:"-"`
	UserID       string `json:"-"`
	Workspace    string `json:"workspace"`
	Timezone     string `json:"timezone"`
	Organization View   `json:"organization"`
	User         View   `json:"user"`
}

// validate checks every input before anything is written.
func (in InitInput) validate() (InitInput, error) {
	in.Timezone = strings.TrimSpace(in.Timezone)
	in.Organization = strings.TrimSpace(in.Organization)
	in.UserName = strings.TrimSpace(in.UserName)
	if in.UserEmail != "" {
		in.UserEmail = NormalEmail(in.UserEmail)
		if p := emailProblem(in.UserEmail); p != "" {
			return in, invalidValue("user-email", "give the address as name@example.com", "%s", p).with("value", in.UserEmail)
		}
	}
	if err := ValidTimezone(in.Timezone); err != nil {
		return in, invalidValue("timezone", "use an IANA zone name such as Europe/Madrid or UTC", "%v", err)
	}
	if Slug(in.Organization) == "" {
		return in, invalidValue("organization", "the name needs at least one letter or digit that has an ASCII form",
			"%q is not a usable organization name", in.Organization)
	}
	if !userKey.MatchString(in.UserKey) {
		return in, invalidValue("user", "use lowercase letters, digits and hyphens, such as diego-petalo",
			"%q is not a valid user key", in.UserKey)
	}
	return in, nil
}

// Init creates the workspace on c: config.json, the own organization, the
// first human user and, last, kravis.json. It validates everything before it
// writes. On an initialized workspace it writes nothing: it returns
// OutcomeExisting when the data matches, and WORKSPACE_EXISTS otherwise. A
// folder where an earlier init stopped is completed, and anything that
// conflicts with the data given is WORKSPACE_EXISTS.
func Init(c storage.Connector, now func() time.Time, in InitInput) (*InitResult, error) {
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	// Another init may write a file between our read and our create. Look
	// again: what it wrote is either the same data, which we adopt, or a
	// conflict. Each pass moves one file on, and there are four.
	var res *InitResult
	for range 5 {
		if res, err = initOnce(c, now, in); !errors.Is(err, storage.ErrExists) {
			break
		}
	}
	return res, err
}

func initOnce(c storage.Connector, now func() time.Time, in InitInput) (*InitResult, error) {
	orgID, userID := "org_"+Slug(in.Organization), "us_"+in.UserKey
	st := storage.NewStore(c, now)

	var marker Marker
	initialized := true
	if err := readFile(c, MarkerPath, &marker); errors.Is(err, storage.ErrNotFound) {
		initialized = false
	} else if errors.Is(err, errInvalidFile) {
		return nil, exists(MarkerPath, err.Error(), "the folder has an invalid "+MarkerPath)
	} else if err != nil {
		return nil, err
	}
	create := !initialized
	var ours []string
	if in.UserEmail != "" {
		switch owner, err := st.KeyOwner(storage.Key{Kind: KeyPersonEmail, Value: in.UserEmail}); {
		case err == nil:
			ours = append(ours, PersonPath(owner))
		case !errors.Is(err, storage.ErrNotFound):
			return nil, err
		}
	}
	if create {
		if err := onlyOurs(st, append(ours, OrganizationPath(orgID), UserPath(userID))); err != nil {
			return nil, err
		}
	}

	var cfg Config
	switch err := readFile(c, ConfigPath, &cfg); {
	case errors.Is(err, storage.ErrNotFound) && create:
		cfg = Config{Timezone: in.Timezone, Catalogs: DefaultCatalogs()}
		data, err := encodeFile(cfg)
		if err == nil {
			_, err = c.Create(ConfigPath, data)
		}
		if err != nil {
			return nil, err
		}
	case err != nil && !errors.Is(err, errInvalidFile) && !errors.Is(err, storage.ErrNotFound):
		return nil, err
	case err != nil:
		return nil, exists(ConfigPath, err.Error(), "the workspace has no valid "+ConfigPath)
	case cfg.Timezone != in.Timezone:
		return nil, exists("timezone", cfg.Timezone, "the workspace uses timezone "+cfg.Timezone)
	}

	org := &Organization{Name: in.Organization, Own: true, Live: true}
	org.ID = orgID
	if err := ensure(st, OrganizationPath(orgID), org, userID, create, func(have *Organization) bool {
		// Its creator is the first user: another user's init was interrupted here.
		return have.Name == in.Organization && have.Own && have.Live && have.CreatedBy == userID
	}, "organization", in.Organization); err != nil {
		return nil, err
	}

	var personID string
	if in.UserEmail != "" {
		var err error
		if personID, err = ensurePerson(st, now, in, userID, create); err != nil {
			return nil, err
		}
	}

	user := &User{Holder: orgID, Kind: KindHuman, Name: in.UserName, Person: personID, Live: true}
	user.ID = userID
	if err := ensure(st, UserPath(userID), user, userID, create, func(have *User) bool {
		return have.Kind == KindHuman && have.Holder == orgID && have.Live &&
			(in.UserName == "" || have.Name == in.UserName) &&
			(personID == "" || have.Person == personID)
	}, "user", in.UserKey); err != nil {
		return nil, err
	}

	var err error
	res := &InitResult{Outcome: OutcomeExisting, UserID: user.ID, Workspace: marker.Workspace, Timezone: cfg.Timezone}
	if res.Organization, err = organizations.view(org, nil, ViewOptions{}); err != nil {
		return nil, err
	}
	if res.User, err = users.view(user, nil, ViewOptions{}); err != nil {
		return nil, err
	}
	if create {
		id, err := typeid.WithPrefix("ws")
		if err != nil {
			return nil, err
		}
		marker = Marker{Workspace: id.String(), SchemaVersion: SchemaVersion}
		data, err := encodeFile(marker)
		if err == nil {
			_, err = c.Create(MarkerPath, data)
		}
		if err != nil {
			return nil, err
		}
		res.Outcome, res.Workspace = OutcomeCreated, marker.Workspace
	}
	return res, nil
}

// ensure makes sure the record is on disk. A record already there must match
// (and then replaces rec in the caller's view); a missing one is created when
// create is true, and a conflict otherwise.
func ensure[T any, P interface {
	*T
	storage.Record
}](st *storage.Store, path string, rec P, actor string, create bool, matches func(P) bool, field, given string) error {
	have := P(new(T))
	_, err := st.Load(path, have)
	switch {
	case err == nil:
		if !matches(have) {
			return exists(field, given, "the workspace already has a different "+field+" at "+path)
		}
		*rec = *have
		return nil
	case !errors.Is(err, storage.ErrNotFound):
		return err
	case !create:
		return exists(field, given, fmt.Sprintf("the workspace already exists and does not have the %s %q", field, given))
	}
	_, _, err = storage.Create(st, rec, actor, func(string) string { return path }, nil)
	return err
}

// ensurePerson returns the ID of the person with the email of the first user.
// One that exists is used unchanged; a missing one is created, named by
// --user-name, when create is true, and a conflict otherwise.
func ensurePerson(st *storage.Store, now func() time.Time, in InitInput, userID string, create bool) (string, error) {
	key := storage.Key{Kind: KeyPersonEmail, Value: in.UserEmail}
	owner, err := st.KeyOwner(key)
	switch {
	case err == nil:
		return owner, nil
	case !errors.Is(err, storage.ErrNotFound):
		return "", err
	case !create:
		return "", exists("user-email", in.UserEmail, fmt.Sprintf("the workspace already exists and no person has the email %q", in.UserEmail))
	}
	ws := &Workspace{Store: st, now: now}
	rec := &Person{Name: in.UserName, Emails: []string{in.UserEmail}, Live: true}
	ex, _, err := people.create(ws, userID, rec, personSlug(in.UserName, rec.Emails))
	if err != nil {
		return "", err
	}
	if ex != nil { // another init created it meanwhile
		return ex.ID, nil
	}
	return rec.ID, nil
}

// onlyOurs refuses a folder whose records are not the ones init would write:
// they belong to something else, or to an init with other data.
func onlyOurs(st *storage.Store, ours []string) error {
	paths, err := st.List("records/")
	if err != nil {
		return err
	}
	for _, p := range paths {
		if !slices.Contains(ours, p) {
			return exists("records", p, "the folder holds records that this init would not write")
		}
	}
	return nil
}

// exists is the WORKSPACE_EXISTS error.
func exists(field, got, message string) *Error {
	e := newError(CodeWorkspaceExists,
		"run init with the values of the existing workspace, or in an empty folder; kravis check shows what is there",
		"%s", message).with("field", field)
	if got != "" {
		_ = e.with("value", got)
	}
	return e
}
