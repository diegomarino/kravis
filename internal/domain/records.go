package domain

import (
	"errors"
	"regexp"

	"github.com/diegomarino/kravis/internal/storage"
)

// User kinds.
const (
	KindHuman = "human"
	KindAgent = "agent"
)

// User is someone who operates Kravis on behalf of one holder.
type User struct {
	storage.Base
	Holder string `json:"holder"`
	Kind   string `json:"kind"`
	// Handler is an agent's handler: a live human of the same holder.
	Handler string `json:"handler,omitempty"`
	// Person is the human's own person, if linked.
	Person string `json:"person,omitempty"`
	Name   string `json:"name,omitempty"`
	Live   bool   `json:"live"`
}

// Organization is a company or group Kravis keeps track of. It has no
// holder: every holder shares it (DIRECTORY-INV-01).
type Organization struct {
	storage.Base
	Name    string   `json:"name"`
	Domains []string `json:"domains,omitempty"`
	Own     bool     `json:"own,omitempty"`
	Live    bool     `json:"live"`
}

// Person is an individual with whom a relationship is kept. It has a name,
// an email or both, and no holder.
type Person struct {
	storage.Base
	Name   string   `json:"name,omitempty"`
	Emails []string `json:"emails,omitempty"`
	// Affiliations are sorted by organization, at most one per organization.
	Affiliations []Affiliation `json:"affiliations,omitempty"`
	Live         bool          `json:"live"`
}

var users = newKind(kind[User, *User]{
	Type: Type{Name: "user", Dir: "users", Prefix: "us_",
		Order: "name compared as its slug, else the ID; then the ID"},
	compact: []compactField{{"id", ""}, {"name", ""}, {"live", "false"}, {"kind", ""}, {"holder", ""}},
	sortKey: func(u *User) string { return firstOf(u.Name, u.ID) },
	live:    func(u *User) bool { return u.Live },
	rules:   userRules,
})

// Types lists every record type that has a declared view and order.
func Types() []Type {
	return []Type{users.Type, organizations.Type, people.Type, interactions.Type, tasks.Type}
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// A user's key is lowercase ASCII words joined by hyphens.
var userKey = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// UserPath is where the user with the given ID lives.
func UserPath(id string) string { return users.Path(id) }

// OrganizationPath is where the organization with the given ID lives.
func OrganizationPath(id string) string { return organizations.Path(id) }

// PersonPath is where the person with the given ID lives.
func PersonPath(id string) string { return people.Path(id) }

// Actor loads the user that acts. The user must exist and be live
// (IDENTITY-INV-05, WORKSPACE-INV-14); otherwise it is ACTOR_NOT_LIVE.
func (ws *Workspace) Actor(id string) (*User, error) {
	notLive := func(why string) *Error {
		return newError(CodeActorNotLive,
			"use a live user's ID, with --actor or KRAVIS_ACTOR, or store one with kravis whoami --set <user>",
			"the actor %q %s", id, why).with("actor", id)
	}
	if !users.validID(id) {
		return nil, notLive("is not a user ID (us_…)")
	}
	u := &User{}
	if _, err := ws.Store.Load(UserPath(id), u); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, notLive("does not exist")
		}
		return nil, err
	}
	if !u.Live {
		return nil, notLive("is not live")
	}
	return u, nil
}
