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
	Name   string `json:"name,omitempty"`
	Live   bool   `json:"live"`
}

// Organization is a company or group Kravis keeps track of.
type Organization struct {
	storage.Base
	Name    string   `json:"name"`
	Domains []string `json:"domains,omitempty"`
	Own     bool     `json:"own,omitempty"`
	Live    bool     `json:"live"`
}

// UserView is the compact view of a user.
type UserView struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Live   bool   `json:"live"`
	Kind   string `json:"kind"`
	Holder string `json:"holder"`
}

// OrganizationView is the compact view of an organization.
type OrganizationView struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Live    bool     `json:"live"`
	Domains []string `json:"domains"`
	Own     bool     `json:"own"`
}

// View returns the user's compact view.
func (u *User) View() UserView {
	return UserView{ID: u.ID, Name: u.Name, Live: u.Live, Kind: u.Kind, Holder: u.Holder}
}

// View returns the organization's compact view; domains is never null.
func (o *Organization) View() OrganizationView {
	d := o.Domains
	if d == nil {
		d = []string{}
	}
	return OrganizationView{ID: o.ID, Name: o.Name, Live: o.Live, Domains: d, Own: o.Own}
}

// A user's key is lowercase ASCII words joined by hyphens.
var userKey = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var userID = regexp.MustCompile(`^us_[a-z0-9]+(-[a-z0-9]+)*$`)

// UserPath is where the user with the given ID lives.
func UserPath(id string) string { return "records/users/" + id + ".json" }

// OrganizationPath is where the organization with the given ID lives.
func OrganizationPath(id string) string { return "records/organizations/" + id + ".json" }

// Actor loads the user that acts. The user must exist and be live
// (IDENTITY-INV-05, WORKSPACE-INV-14); otherwise it is ACTOR_NOT_LIVE.
func (ws *Workspace) Actor(id string) (*User, error) {
	notLive := func(why string) *Error {
		return newError(CodeActorNotLive,
			"use a live user's ID, with --actor or KRAVIS_ACTOR, or store one with kravis whoami --set <user>",
			"the actor %q %s", id, why).with("actor", id)
	}
	if !userID.MatchString(id) {
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
