package main

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diegomarino/kravis/internal/domain"
	"github.com/diegomarino/kravis/internal/storage"
)

const userEmail = "ana@example.com"

func readPerson(t *testing.T, e *env, id string) domain.Person {
	t.Helper()
	var p domain.Person
	raw, err := os.ReadFile(filepath.Join(e.Workspace, "records/people/"+id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Decode(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func readUser(t *testing.T, e *env) domain.User {
	t.Helper()
	var u domain.User
	raw, err := os.ReadFile(filepath.Join(e.Workspace, "records/users/"+userID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Decode(raw, &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestInitUserEmailCreatesAndLinksThePerson(t *testing.T) {
	e := newEnv(t)
	r := e.init("--user-email", " Ana@Example.com ")
	if r.Code != 0 || r.JSON(t)["outcome"] != "created" {
		t.Fatalf("code %d, %q %q", r.Code, r.Stdout, r.Stderr)
	}
	u := readUser(t, e)
	if u.Person != "pe_ana-example" {
		t.Fatalf("user person = %q", u.Person)
	}
	p := readPerson(t, e, "pe_ana-example")
	if p.Name != userName || len(p.Emails) != 1 || p.Emails[0] != userEmail || !p.Live || p.CreatedBy != userID {
		t.Errorf("person = %+v", p)
	}

	files := e.Files()
	r = e.init("--user-email", userEmail)
	if r.Code != 0 || r.JSON(t)["outcome"] != "existing" || !maps.Equal(files, e.Files()) {
		t.Errorf("rerun: %q %q", r.Stdout, r.Stderr)
	}
}

func TestInitUserEmailWithoutANameCreatesAPersonWithOnlyTheEmail(t *testing.T) {
	e := newEnv(t)
	r := e.run("init", "--timezone", tz, "--organization", orgName, "--user", userKey, "--user-email", userEmail)
	if r.Code != 0 {
		t.Fatalf("code %d, %q", r.Code, r.Stderr)
	}
	p := readPerson(t, e, "pe_ana-example")
	if p.Name != "" || len(p.Emails) != 1 || p.Emails[0] != userEmail {
		t.Errorf("person = %+v", p)
	}
	if readUser(t, e).Person != "pe_ana-example" {
		t.Errorf("not linked")
	}
}

func TestInitUserEmailLinksAnExistingPersonUnchanged(t *testing.T) {
	e := newEnv(t)
	// An init that stopped before the user left the person behind.
	e.Connector = &failAt{Connector: storage.NewLocal(e.Workspace), sub: "records/users/"}
	wantError(t, e.init("--user-email", userEmail), "INTERNAL", 1)
	e.Connector = nil
	before := readPerson(t, e, "pe_ana-example")

	r := e.init("--user-email", userEmail, "--user-name", userName)
	if r.Code != 0 || r.JSON(t)["outcome"] != "created" {
		t.Fatalf("code %d, %q %q", r.Code, r.Stdout, r.Stderr)
	}
	if readUser(t, e).Person != "pe_ana-example" {
		t.Errorf("not linked")
	}
	if got := readPerson(t, e, "pe_ana-example"); got.Version != before.Version || got.Name != before.Name {
		t.Errorf("person changed: %+v", got)
	}
}

func TestInitUserEmailThatDoesNotMatchIsRefused(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	files := e.Files()
	wantError(t, e.init("--user-email", userEmail), "WORKSPACE_EXISTS", 2) // the user has no person
	if !maps.Equal(files, e.Files()) {
		t.Error("files changed")
	}

	e = newEnv(t)
	e.mustInitWith("--user-email", userEmail)
	files = e.Files()
	wantError(t, e.init("--user-email", "other@example.com"), "WORKSPACE_EXISTS", 2)
	if !maps.Equal(files, e.Files()) {
		t.Error("files changed")
	}
}

func TestInitUserEmailIsValidatedBeforeWriting(t *testing.T) {
	e := newEnv(t)
	eo := wantError(t, e.init("--user-email", "not-an-email"), "INVALID_VALUE", 2)
	if d := obj(t, eo, "details"); d["field"] != "user-email" {
		t.Errorf("details = %v", d)
	}
	if got := e.Files(); len(got) != 0 {
		t.Errorf("a refused init left files: %v", got)
	}
}

func (e *env) mustInitWith(extra ...string) {
	e.t.Helper()
	if r := e.init(extra...); r.Code != 0 {
		e.t.Fatalf("init: code %d, stderr %q", r.Code, r.Stderr)
	}
}

func TestInitUserEmailCompletesAPersonWhoseKeyWasWrittenFirst(t *testing.T) {
	e := newEnv(t)
	// A crash leaves the key behind: its cleanup never runs.
	e.Connector = &keepKeys{failAt{Connector: storage.NewLocal(e.Workspace), sub: "records/people/"}}
	wantError(t, e.init("--user-email", userEmail), "INTERNAL", 1)
	e.Connector = nil
	if _, err := os.Stat(filepath.Join(e.Workspace, "records/people/pe_ana-example.json")); err == nil {
		t.Fatal("the person was written")
	}
	keys, _ := storage.NewLocal(e.Workspace).List("keys/")
	if len(keys) != 1 {
		t.Fatalf("want the orphaned key, got %v", keys)
	}
	if r := e.init("--user-email", userEmail); r.Code != 0 || r.JSON(t)["outcome"] != "created" {
		t.Fatalf("code %d, %q %q", r.Code, r.Stdout, r.Stderr)
	}
	if readUser(t, e).Person != "pe_ana-example" || readPerson(t, e, "pe_ana-example").Emails[0] != userEmail {
		t.Error("the person was not completed and linked")
	}
}

// keepKeys is a connector that never deletes a key, as a crashed process
// would not.
type keepKeys struct{ failAt }

func (c *keepKeys) Delete(p string, tok storage.Token) error {
	if strings.HasPrefix(p, "keys/") {
		return nil
	}
	return c.Connector.Delete(p, tok)
}
