package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
	"github.com/diegomarino/kravis/internal/storage"
)

// Fixtures: invented names, example.com only.
const (
	tz       = "Europe/Madrid"
	orgName  = "Acme Example"
	orgID    = "org_acme-example"
	userKey  = "ana-acme"
	userID   = "us_ana-acme"
	userName = "Ana Example"
)

func (e *env) init(extra ...string) result {
	e.t.Helper()
	args := []string{"init", "--timezone", tz, "--organization", orgName, "--user", userKey, "--user-name", userName}
	return e.run(append(args, extra...)...)
}

func (e *env) mustInit() {
	e.t.Helper()
	if r := e.init(); r.Code != 0 {
		e.t.Fatalf("init: code %d, stderr %q", r.Code, r.Stderr)
	}
}

func (e *env) localConfig() string { return filepath.Join(e.Home, ".config", "kravis", "config.json") }

func obj(t *testing.T, v any, key string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want an object, got %v", v)
	}
	o, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not an object in %v", key, m)
	}
	return o
}

func errorOf(t *testing.T, r result) map[string]any {
	t.Helper()
	return obj(t, r.ErrorJSON(t), "error")
}

func dataOf(t *testing.T, r result) map[string]any { return obj(t, r.JSON(t), "data") }

// wantError checks one error: stdout empty, the code and the exit code.
func wantError(t *testing.T, r result, code string, exit int) map[string]any {
	t.Helper()
	if r.Stdout != "" {
		t.Errorf("an error must leave stdout empty, got %q", r.Stdout)
	}
	if r.Code != exit {
		t.Errorf("exit code %d, want %d (stderr %q)", r.Code, exit, r.Stderr)
	}
	eo := errorOf(t, r)
	if eo["code"] != code {
		t.Errorf("error code %v, want %s (%v)", eo["code"], code, eo)
	}
	return eo
}

func TestWORKSPACE_AC_01_OneJSONObjectOutsideATTY(t *testing.T) {
	e := newEnv(t)
	e.mustInit()

	t.Run("success: data on stdout, nothing on stderr", func(t *testing.T) {
		for _, args := range [][]string{{"--version"}, {"whoami"}, {"check"}} {
			r := e.run(args...)
			if r.Code != 0 || r.Stderr != "" {
				t.Fatalf("%v: code %d, stderr %q", args, r.Code, r.Stderr)
			}
			if _, ok := r.JSON(t)["data"]; !ok {
				t.Errorf("%v: no data in %q", args, r.Stdout)
			}
			if strings.Count(r.Stdout, "\n") != 1 {
				t.Errorf("%v: want one line, got %q", args, r.Stdout)
			}
		}
	})

	t.Run("errors and parser failures: stderr only, no usage text", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			args []string
			code string
		}{
			{"unknown command", []string{"nope"}, "INVALID_VALUE"},
			{"unknown flag", []string{"check", "--nope"}, "UNKNOWN_FIELD"},
			{"unknown shorthand", []string{"check", "-x"}, "UNKNOWN_FIELD"},
			{"missing argument", []string{"init", "--timezone"}, "INVALID_VALUE"},
			{"missing required flag", []string{"init", "--timezone", tz}, "INVALID_VALUE"},
			{"malformed flag value", []string{"check", "--verbose=maybe"}, "INVALID_VALUE"},
			{"unexpected argument", []string{"check", "extra"}, "INVALID_VALUE"},
			{"bad output format", []string{"check", "--output", "yaml"}, "INVALID_VALUE"},
			{"table off a TTY", []string{"check", "--output", "table"}, "INVALID_VALUE"},
			{"help command", []string{"help"}, "INVALID_VALUE"},
			{"completion protocol", []string{"__complete", ""}, "INVALID_VALUE"},
			{"completion protocol after a flag", []string{"--output", "json", "__complete", ""}, "INVALID_VALUE"},
			{"completion without descriptions", []string{"--verbose", "__completeNoDesc", "wh"}, "INVALID_VALUE"},
			{"completion command", []string{"completion", "bash"}, "INVALID_VALUE"},
			{"no command", nil, "INVALID_VALUE"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				r := e.run(tt.args...)
				eo := wantError(t, r, tt.code, 2)
				if strings.Contains(r.Stderr, "Usage:") || strings.Contains(r.Stderr, "\n  ") {
					t.Errorf("usage text leaked: %q", r.Stderr)
				}
				if strings.Count(r.Stderr, "\n") != 1 {
					t.Errorf("want one line, got %q", r.Stderr)
				}
				if eo["message"] == "" {
					t.Error("empty message")
				}
			})
		}
	})

	t.Run("an unknown command lists the valid ones; an unknown flag names the field", func(t *testing.T) {
		d := obj(t, errorOf(t, e.run("nope")), "details")
		if allowed, _ := d["allowed"].([]any); len(allowed) < 3 {
			t.Errorf("allowed = %v", d["allowed"])
		}
		d = obj(t, errorOf(t, e.run("check", "--nope")), "details")
		if d["field"] != "nope" {
			t.Errorf("details = %v", d)
		}
	})

	t.Run("only --help prints text", func(t *testing.T) {
		for _, args := range [][]string{{"--help"}, {"init", "--help"}, {"whoami", "-h"}} {
			r := e.run(args...)
			if r.Code != 0 || !strings.Contains(r.Stdout, "Usage:") || r.Stderr != "" {
				t.Errorf("%v: code %d, stdout %q, stderr %q", args, r.Code, r.Stdout, r.Stderr)
			}
		}
	})

	t.Run("no HTML escaping", func(t *testing.T) {
		r := e.run("--actor", "us_nobody", "whoami")
		wantError(t, r, "ACTOR_NOT_LIVE", 2)
		if !strings.Contains(r.Stderr, "<user>") || strings.Contains(r.Stderr, `\u003c`) {
			t.Errorf("stderr = %q", r.Stderr)
		}
	})

	t.Run("an empty list is [] and never null", func(t *testing.T) {
		r := e.run("check")
		if !strings.Contains(r.Stdout, `"data":[]`) {
			t.Errorf("got %q", r.Stdout)
		}
		if got := nonNull(nil); got == nil {
			t.Error("nil data became null")
		}
		if got := nonNull([]string(nil)); got == nil || len(got.([]string)) != 0 {
			t.Errorf("nil list: %v", got)
		}
		if got := nonNull(map[string]int(nil)); got == nil {
			t.Error("nil map became null")
		}
		if got := nonNull((*int)(nil)); got == nil {
			t.Error("nil pointer became null")
		}
	})

	t.Run("on a TTY a table, unless JSON is forced", func(t *testing.T) {
		e.TTY = true
		defer func() { e.TTY = false }()
		r := e.run("whoami")
		if r.Code != 0 || strings.HasPrefix(r.Stdout, "{") || !strings.Contains(r.Stdout, "actor") {
			t.Errorf("table: code %d, %q", r.Code, r.Stdout)
		}
		for _, force := range [][]string{{"--output", "json"}} {
			if got := e.run(append([]string{"whoami"}, force...)...); !strings.HasPrefix(got.Stdout, "{") {
				t.Errorf("--output json: %q", got.Stdout)
			}
		}
		t.Setenv("KRAVIS_OUTPUT", "json")
		if got := e.run("whoami"); !strings.HasPrefix(got.Stdout, "{") {
			t.Errorf("KRAVIS_OUTPUT=json: %q", got.Stdout)
		}
		t.Setenv("KRAVIS_OUTPUT", "")
		r = e.run("check") // a list
		if r.Code != 0 || strings.HasPrefix(r.Stdout, "{") {
			t.Errorf("list table: %q", r.Stdout)
		}
	})
}

func TestVersionFollowsTheBuild(t *testing.T) {
	defer func(v string) { version = v }(version)
	for in, want := range map[string]string{"v0.2.0": "0.2.0", "0.1.0": "0.1.0", "dev": "dev", "": "dev"} {
		version = in
		if got := buildVersion(); got != want {
			t.Errorf("version %q gives %q, want %q", in, got, want)
		}
	}
}

func TestWORKSPACE_AC_02_EveryErrorIsComplete(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	for name, r := range map[string]result{
		"invalid value":    e.run("init", "--timezone", "Mars/Base", "--organization", "X", "--user", "x"),
		"workspace exists": e.run("init", "--timezone", tz, "--organization", "Other", "--user", userKey),
		"actor not live":   e.run("--actor", "us_nobody", "whoami"),
		"unknown flag":     e.run("check", "--nope"),
	} {
		eo := errorOf(t, r)
		for _, k := range []string{"code", "message", "details", "retryable"} {
			if _, ok := eo[k]; !ok {
				t.Errorf("%s: no %s in %v", name, k, eo)
			}
		}
		if _, ok := eo["details"].(map[string]any); !ok {
			t.Errorf("%s: details is not an object: %v", name, eo["details"])
		}
		if eo["retryable"] != false {
			t.Errorf("%s: retryable = %v", name, eo["retryable"])
		}
		if h, _ := eo["hint"].(string); h == "" {
			t.Errorf("%s: a next step is not obvious from the details, so a hint is needed: %v", name, eo)
		}
	}

	// No workspace is STORAGE_UNAVAILABLE, but waiting never fixes it.
	t.Chdir(t.TempDir())
	eo := wantError(t, e.run("check"), "STORAGE_UNAVAILABLE", 5)
	if eo["retryable"] != false {
		t.Errorf("retryable = %v", eo["retryable"])
	}
	if h, _ := eo["hint"].(string); !strings.Contains(h, "kravis init") {
		t.Errorf("hint = %q", h)
	}
}

// errConnector fails every call with err.
type errConnector struct {
	storage.Connector
	err error
}

func (c errConnector) Get(string) ([]byte, storage.Token, error) { return nil, "", c.err }

func TestWORKSPACE_AC_03_ExitCodes(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	if r := e.run("check"); r.Code != 0 {
		t.Errorf("success: %d", r.Code)
	}
	if r := e.init(); r.Code != 0 || dataOf(t, r)["workspace"] == nil {
		t.Errorf("existing is a success: %d", r.Code)
	}
	wantError(t, e.run("nope"), "INVALID_VALUE", 2)
	wantError(t, e.run("--actor", "us_nobody", "whoami"), "ACTOR_NOT_LIVE", 2)
	wantError(t, e.init("--organization", "Other"), "WORKSPACE_EXISTS", 2)

	// What the library returns maps to the exit codes 1, 3, 4 and 5.
	for name, tt := range map[string]struct {
		err  error
		code string
		exit int
	}{
		"internal":      {errors.New("boom"), "INTERNAL", 1},
		"not found":     {storage.ErrNotFound, "NOT_FOUND", 3},
		"version":       {storage.ErrVersionConflict, "VERSION_CONFLICT", 4},
		"dedup":         {storage.ErrKeyConflict, "DEDUP_CONFLICT", 4},
		"referenced":    {&domain.Error{Code: domain.CodeReferenced, Message: "x"}, "REFERENCED", 4},
		"unavailable":   {storage.ErrUnavailable, "STORAGE_UNAVAILABLE", 5},
		"holder":        {&domain.Error{Code: domain.CodeHolderMismatch, Message: "x"}, "HOLDER_MISMATCH", 2},
		"unknown code":  {&domain.Error{Code: "SOMETHING_NEW", Message: "x"}, "SOMETHING_NEW", 1},
		"wrapped sentl": {errors.Join(errors.New("ctx"), storage.ErrNotFound), "NOT_FOUND", 3},
	} {
		var stderr strings.Builder
		if got := writeError(&stderr, tt.err); got != tt.exit {
			t.Errorf("%s: exit %d, want %d", name, got, tt.exit)
		}
		if !strings.Contains(stderr.String(), `"code":"`+tt.code+`"`) {
			t.Errorf("%s: %s", name, stderr.String())
		}
	}

	// An unexpected failure of the storage is INTERNAL, exit 1.
	e.Connector = errConnector{storage.NewMemory(), errors.New("disk on fire")}
	wantError(t, e.run("whoami"), "INTERNAL", 1)

	// A panic is INTERNAL too, with nothing on stdout.
	a := newApp(e.deps())
	root := a.root()
	root.AddCommand(&cobra.Command{Use: "boom", RunE: func(*cobra.Command, []string) error { panic("kaboom") }})
	wantError(t, e.runRoot(a, root, "boom"), "INTERNAL", 1)
}

func TestExitCodeMapCoversEveryErrorCode(t *testing.T) {
	for _, c := range []string{
		domain.CodeInternal, domain.CodeMissingActor, domain.CodeActorNotLive, domain.CodeUnknownField,
		domain.CodeInvalidValue, domain.CodeHolderMismatch, domain.CodeNotFound, domain.CodeVersionConflict,
		domain.CodeDedupConflict, domain.CodeReferenced, domain.CodeWorkspaceExists, domain.CodeStorageUnavailable,
	} {
		if _, ok := exitCodes[c]; !ok {
			t.Errorf("no exit code for %s", c)
		}
	}
}

func TestWORKSPACE_AC_12_InitValidatesBeforeWriting(t *testing.T) {
	e := newEnv(t)
	for name, tt := range map[string]struct {
		field string
		args  []string
	}{
		"timezone":     {"timezone", []string{"init", "--timezone", "Mars/Base", "--organization", orgName, "--user", userKey}},
		"local":        {"timezone", []string{"init", "--timezone", "Local", "--organization", orgName, "--user", userKey}},
		"organization": {"organization", []string{"init", "--timezone", tz, "--organization", "  ", "--user", userKey}},
		"greek":        {"organization", []string{"init", "--timezone", tz, "--organization", "Εταιρεία", "--user", userKey}},
		"user":         {"user", []string{"init", "--timezone", tz, "--organization", orgName, "--user", "Ana Example"}},
		"missing":      {"", []string{"init", "--timezone", tz}},
		"empty":        {"user", []string{"init", "--timezone", tz, "--organization", orgName, "--user", ""}},
	} {
		before := e.Files()
		eo := wantError(t, e.run(tt.args...), "INVALID_VALUE", 2)
		if d := obj(t, eo, "details"); tt.field != "" && d["field"] != tt.field {
			t.Errorf("%s: details = %v", name, d)
		}
		if after := e.Files(); !maps.Equal(before, after) {
			t.Errorf("%s: files changed: %v", name, after)
		}
	}
	if got := e.Files(); len(got) != 0 {
		t.Errorf("a refused init left files: %v", got)
	}
}

func TestWORKSPACE_AC_12_InitCreatesExistsAndRefuses(t *testing.T) {
	e := newEnv(t)
	r := e.init()
	if r.Code != 0 || r.Stderr != "" || r.JSON(t)["outcome"] != "created" {
		t.Fatalf("code %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	data := dataOf(t, r)
	if ws, _ := data["workspace"].(string); !strings.HasPrefix(ws, "ws_") {
		t.Errorf("workspace = %v", data["workspace"])
	}
	if obj(t, data, "organization")["id"] != orgID || obj(t, data, "user")["id"] != userID {
		t.Errorf("data = %v", data)
	}

	files := e.Files()
	for _, want := range []string{"ws:kravis.json", "ws:config.json", "ws:records/organizations/" + orgID + ".json",
		"ws:records/users/" + userID + ".json", "home:.config/kravis/config.json"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s in %v", want, files)
		}
	}
	for _, p := range []string{"config.json", "records/users/" + userID + ".json", "records/organizations/" + orgID + ".json"} {
		raw, _ := os.ReadFile(filepath.Join(e.Workspace, p))
		t.Logf("%s:\n%s", p, raw)
	}
	var user domain.User
	raw, _ := os.ReadFile(filepath.Join(e.Workspace, "records/users/"+userID+".json"))
	if err := storage.Decode(raw, &user); err != nil {
		t.Fatal(err)
	}
	if user.Holder != orgID || user.Kind != "human" || !user.Live || user.Name != userName ||
		user.CreatedBy != userID || user.UpdatedBy != userID || user.Version != 1 {
		t.Errorf("user = %+v", user)
	}
	var org domain.Organization
	raw, _ = os.ReadFile(filepath.Join(e.Workspace, "records/organizations/"+orgID+".json"))
	if err := storage.Decode(raw, &org); err != nil {
		t.Fatal(err)
	}
	if !org.Own || !org.Live || org.Name != orgName || org.CreatedBy != userID {
		t.Errorf("organization = %+v", org)
	}
	cfg, _ := os.ReadFile(filepath.Join(e.Workspace, "config.json"))
	if !strings.Contains(string(cfg), `"timezone": "`+tz+`"`) || !strings.Contains(string(cfg), `"follow-up"`) {
		t.Errorf("config.json = %s", cfg)
	}

	// The same init again: existing, no file changed.
	r = e.init()
	if r.Code != 0 || r.JSON(t)["outcome"] != "existing" {
		t.Fatalf("rerun: code %d, %q %q", r.Code, r.Stdout, r.Stderr)
	}
	if got := e.Files(); !maps.Equal(files, got) {
		t.Errorf("a matching rerun changed files:\n%v\n%v", files, got)
	}
	// Without --user-name it still matches.
	if r := e.run("init", "--timezone", tz, "--organization", orgName, "--user", userKey); r.JSON(t)["outcome"] != "existing" {
		t.Errorf("without a name: %q", r.Stdout)
	}

	// Anything else is refused with a hint, and nothing changes.
	for name, args := range map[string][]string{
		"organization": {"--organization", "Other Example"},
		"user":         {"--user", "bo-acme"},
		"user name":    {"--user-name", "Someone Else"},
		"timezone":     {"--timezone", "UTC"},
	} {
		r := e.init(args...)
		eo := wantError(t, r, "WORKSPACE_EXISTS", 2)
		if eo["hint"] == nil || eo["hint"] == "" {
			t.Errorf("%s: no hint", name)
		}
		if got := e.Files(); !maps.Equal(files, got) {
			t.Errorf("%s: files changed", name)
		}
	}
}

func TestInitRerunKeepsOrFillsTheLocalActor(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	if err := os.Remove(e.localConfig()); err != nil { // another machine
		t.Fatal(err)
	}
	if r := e.init(); r.JSON(t)["outcome"] != "existing" {
		t.Fatalf("%q", r.Stdout)
	}
	if got := dataOf(t, e.run("whoami")); got["actor"] != userID || got["source"] != "configuration" {
		t.Errorf("whoami = %v", got)
	}
}

// failAt makes the nth Create of a path containing sub fail, once.
type failAt struct {
	storage.Connector
	sub  string
	done bool
}

func (c *failAt) Create(p string, data []byte) (storage.Token, error) {
	if !c.done && strings.Contains(p, c.sub) {
		c.done = true
		return "", errors.New("injected failure")
	}
	return c.Connector.Create(p, data)
}

func TestWORKSPACE_AC_12_InterruptedInitIsCompleted(t *testing.T) {
	for name, tt := range map[string]struct {
		failOn string
		left   []string // what the interruption leaves behind
	}{
		"before the organization":   {"records/organizations/", []string{"ws:config.json"}},
		"after the organization":    {"records/users/", []string{"ws:config.json", "ws:records/organizations/" + orgID + ".json"}},
		"before kravis.json":        {"kravis.json", []string{"ws:config.json", "ws:records/organizations/" + orgID + ".json", "ws:records/users/" + userID + ".json"}},
		"before config.json (none)": {"config.json", nil},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.Connector = &failAt{Connector: storage.NewLocal(e.Workspace), sub: tt.failOn}
			r := e.init()
			wantError(t, r, "INTERNAL", 1)
			left := e.Files()
			if _, ok := left["ws:kravis.json"]; ok {
				t.Fatal("an interrupted init wrote kravis.json")
			}
			if len(left) != len(tt.left) {
				t.Errorf("left %v, want %v", left, tt.left)
			}
			for _, want := range tt.left {
				if _, ok := left[want]; !ok {
					t.Errorf("missing %s in %v", want, left)
				}
			}

			// A rerun completes what is missing and keeps what is there.
			r = e.init()
			if r.Code != 0 || r.JSON(t)["outcome"] != "created" {
				t.Fatalf("rerun: code %d, %q %q", r.Code, r.Stdout, r.Stderr)
			}
			done := e.Files()
			for _, p := range tt.left {
				if left[p] != done[p] {
					t.Errorf("%s was rewritten", p)
				}
			}
			orgs, users := 0, 0
			for p := range done {
				switch {
				case strings.HasPrefix(p, "ws:records/organizations/"):
					orgs++
				case strings.HasPrefix(p, "ws:records/users/"):
					users++
				}
			}
			if orgs != 1 || users != 1 || len(done) != 5 {
				t.Errorf("want one organization and one user, got %v", done)
			}
			var org domain.Organization
			raw, _ := os.ReadFile(filepath.Join(e.Workspace, "records/organizations/"+orgID+".json"))
			if err := storage.Decode(raw, &org); err != nil || org.CreatedBy != userID || org.UpdatedBy != userID {
				t.Errorf("attribution: %+v %v", org, err)
			}

			// And one more rerun changes nothing.
			if r := e.init(); r.JSON(t)["outcome"] != "existing" || !maps.Equal(done, e.Files()) {
				t.Errorf("a further rerun changed things: %q", r.Stdout)
			}
		})
	}
}

func TestInterruptedInitRefusesOtherData(t *testing.T) {
	e := newEnv(t)
	e.Connector = &failAt{Connector: storage.NewLocal(e.Workspace), sub: "records/users/"}
	wantError(t, e.init(), "INTERNAL", 1)
	left := e.Files()
	e.Connector = nil
	for name, args := range map[string][]string{
		"another organization": {"--organization", "Other Example"},
		"another timezone":     {"--timezone", "UTC"},
	} {
		wantError(t, e.init(args...), "WORKSPACE_EXISTS", 2)
		if !maps.Equal(left, e.Files()) {
			t.Errorf("%s: files changed", name)
		}
	}
}

func TestInitRefusesFoldersItDoesNotUnderstand(t *testing.T) {
	t.Run("an invalid kravis.json", func(t *testing.T) {
		e := newEnv(t)
		must(t, os.WriteFile("kravis.json", []byte("{not json"), 0o600))
		wantError(t, e.init(), "WORKSPACE_EXISTS", 2)
	})
	t.Run("an invalid config.json", func(t *testing.T) {
		e := newEnv(t)
		e.mustInit()
		must(t, os.WriteFile("config.json", []byte(`{"timezone":"Mars/Base"}`), 0o600))
		wantError(t, e.init(), "WORKSPACE_EXISTS", 2)
	})
	t.Run("a missing user in an initialized workspace", func(t *testing.T) {
		e := newEnv(t)
		e.mustInit()
		must(t, os.Remove("records/users/"+userID+".json"))
		wantError(t, e.init(), "WORKSPACE_EXISTS", 2)
		if _, err := os.Stat("records/users/" + userID + ".json"); err == nil {
			t.Error("init recreated a user in an initialized workspace")
		}
	})
	t.Run("other records in an uninitialized folder", func(t *testing.T) {
		e := newEnv(t)
		must(t, os.MkdirAll("records/people", 0o755))
		must(t, os.WriteFile("records/people/pe_x.json", []byte("{}"), 0o600))
		wantError(t, e.init(), "WORKSPACE_EXISTS", 2)
	})
	t.Run("a workspace folder from KRAVIS_WORKSPACE", func(t *testing.T) {
		e := newEnv(t)
		target := filepath.Join(t.TempDir(), "elsewhere")
		t.Setenv("KRAVIS_WORKSPACE", target)
		if r := e.init(); r.Code != 0 {
			t.Fatalf("%q", r.Stderr)
		}
		if _, err := os.Stat(filepath.Join(target, "kravis.json")); err != nil {
			t.Error(err)
		}
		if got := e.Files(); got["ws:kravis.json"] != "" {
			t.Error("init wrote in the current folder")
		}
	})
	t.Run("an unwritable folder", func(t *testing.T) {
		e := newEnv(t)
		e.Connector = errConnector{storage.NewMemory(), errors.New("no")}
		wantError(t, e.init(), "INTERNAL", 1)
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceDiscovery(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	sub := filepath.Join(e.Workspace, "records", "users")
	t.Chdir(sub)
	if r := e.run("check"); r.Code != 0 {
		t.Errorf("from a subfolder: %q", r.Stderr)
	}
	// KRAVIS_WORKSPACE wins over the folder, and a folder that is no workspace fails.
	other := t.TempDir()
	t.Setenv("KRAVIS_WORKSPACE", other)
	wantError(t, e.run("whoami"), "STORAGE_UNAVAILABLE", 5)
	if r := e.run("check"); r.Code != 6 { // check says what is missing
		t.Errorf("check on a folder that is no workspace: %d", r.Code)
	}
	t.Setenv("KRAVIS_WORKSPACE", e.Workspace)
	t.Chdir(t.TempDir())
	if r := e.run("check"); r.Code != 0 {
		t.Errorf("KRAVIS_WORKSPACE: %q", r.Stderr)
	}
	// An invalid kravis.json stops every command but check.
	must(t, os.WriteFile(filepath.Join(e.Workspace, "kravis.json"), []byte(`{"workspace":"x"}`), 0o600))
	wantError(t, e.run("whoami"), "STORAGE_UNAVAILABLE", 5)
	if r := e.run("check"); r.Code != 6 {
		t.Errorf("check: %d", r.Code)
	}
}

func TestIDENTITY_INV_04_ActorPrecedenceAndMissingActor(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	addUser(t, e, "bo-acme", true)
	addUser(t, e, "cy-acme", true)

	who := func(args ...string) map[string]any { return dataOf(t, e.run(append(args, "whoami")...)) }

	// The configuration alone.
	if got := who(); got["actor"] != userID || got["source"] != "configuration" || got["holder"] != orgID {
		t.Errorf("configuration: %v", got)
	}
	// The environment beats the configuration.
	t.Setenv("KRAVIS_ACTOR", "us_bo-acme")
	if got := who(); got["actor"] != "us_bo-acme" || got["source"] != "environment" {
		t.Errorf("environment: %v", got)
	}
	// The flag beats both.
	if got := who("--actor", "us_cy-acme"); got["actor"] != "us_cy-acme" || got["source"] != "flag" {
		t.Errorf("flag: %v", got)
	}
	// An empty flag falls through.
	if got := who("--actor", ""); got["source"] != "environment" {
		t.Errorf("empty flag: %v", got)
	}

	// With no actor anywhere a write is refused, naming both options, and a read is not.
	t.Setenv("KRAVIS_ACTOR", "")
	must(t, os.Remove(e.localConfig()))
	a := newApp(e.deps())
	root := a.root()
	var wrote bool
	root.AddCommand(&cobra.Command{Use: "poke", RunE: func(cmd *cobra.Command, _ []string) error {
		ws, err := a.open()
		if err != nil {
			return err
		}
		if _, err := a.actor(ws, true); err != nil {
			return err
		}
		wrote = true
		return nil
	}})
	eo := wantError(t, e.runRoot(a, root, "poke"), "MISSING_ACTOR", 2)
	if hint, _ := eo["hint"].(string); !strings.Contains(hint, "--actor") || !strings.Contains(hint, "KRAVIS_ACTOR") {
		t.Errorf("hint = %q", eo["hint"])
	}
	if wrote {
		t.Error("the write ran without an actor")
	}
	if r := e.run("check"); r.Code != 0 {
		t.Errorf("a read needs no actor: %q", r.Stderr)
	}
	wantError(t, e.run("whoami"), "MISSING_ACTOR", 2)
}

func TestWhoamiSet(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	addUser(t, e, "bo-acme", true)
	r := e.run("whoami", "--set", "us_bo-acme")
	if r.Code != 0 || r.JSON(t)["outcome"] != "updated" {
		t.Fatalf("%q %q", r.Stdout, r.Stderr)
	}
	if got := dataOf(t, e.run("whoami")); got["actor"] != "us_bo-acme" || got["source"] != "configuration" {
		t.Errorf("whoami = %v", got)
	}
	if r := e.run("whoami", "--set", "us_bo-acme"); r.JSON(t)["outcome"] != "unchanged" {
		t.Errorf("same value: %q", r.Stdout)
	}
	before := e.Files()
	wantError(t, e.run("whoami", "--set", "us_nobody"), "ACTOR_NOT_LIVE", 2)
	if !maps.Equal(before, e.Files()) {
		t.Error("a refused --set changed the configuration")
	}
	// The other workspaces' entries stay.
	must(t, os.WriteFile(e.localConfig(), []byte(`{"workspaces":{"ws_other":{"actor":"us_z"}}}`), 0o600))
	if r := e.run("whoami", "--set", userID); r.Code != 0 {
		t.Fatal(r.Stderr)
	}
	if raw, _ := os.ReadFile(e.localConfig()); !strings.Contains(string(raw), "ws_other") {
		t.Errorf("other workspaces lost: %s", raw)
	}
}

func TestLocalConfigurationProblems(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	must(t, os.WriteFile(e.localConfig(), []byte("{broken"), 0o600))
	wantError(t, e.run("whoami"), "STORAGE_UNAVAILABLE", 5)
	// A flag or the environment does not need the file.
	if r := e.run("--actor", userID, "whoami"); r.Code != 0 {
		t.Errorf("flag: %q", r.Stderr)
	}
	must(t, os.Remove(e.localConfig()))
	dir := filepath.Dir(e.localConfig())
	must(t, os.Chmod(dir, 0o500))
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		wantError(t, e.run("whoami", "--set", userID), "STORAGE_UNAVAILABLE", 5)
	}
}

func TestIDENTITY_INV_05_ActorMustExist(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	for _, id := range []string{"us_nobody", "nobody", "us_", "../records/users/" + userID, "org_acme-example"} {
		wantError(t, e.run("--actor", id, "whoami"), "ACTOR_NOT_LIVE", 2)
	}
	t.Setenv("KRAVIS_ACTOR", "us_nobody")
	wantError(t, e.run("whoami"), "ACTOR_NOT_LIVE", 2)
	t.Setenv("KRAVIS_ACTOR", "")
	// A default actor that no longer exists is refused too.
	must(t, os.Remove("records/users/"+userID+".json"))
	wantError(t, e.run("whoami"), "ACTOR_NOT_LIVE", 2)
}

func TestWORKSPACE_INV_14_OnlyALiveUserCanAct(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	addUser(t, e, "dee-acme", false)
	eo := wantError(t, e.run("--actor", "us_dee-acme", "whoami"), "ACTOR_NOT_LIVE", 2)
	if !strings.Contains(eo["message"].(string), "not live") {
		t.Errorf("message = %v", eo["message"])
	}
	wantError(t, e.run("whoami", "--set", "us_dee-acme"), "ACTOR_NOT_LIVE", 2)
	// Deactivating the stored actor stops it acting.
	addUser(t, e, "ana-acme", false)
	wantError(t, e.run("whoami"), "ACTOR_NOT_LIVE", 2)
}

// addUser writes a user record by hand, as a later command will.
func addUser(t *testing.T, e *env, key string, live bool) {
	t.Helper()
	st := storage.NewStore(storage.NewLocal(e.Workspace), nil)
	u := &domain.User{Holder: orgID, Kind: "human", Live: live}
	u.ID = "us_" + key
	p := domain.UserPath(u.ID)
	if _, err := st.Load(p, &domain.User{}); err == nil {
		if _, err := storage.Update(st, p, userID, func(x *domain.User) error { x.Live = live; return nil }, nil); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, _, err := storage.Create(st, u, userID, func(string) string { return p }, nil); err != nil {
		t.Fatal(err)
	}
}

func TestVerboseLogsGoToStderr(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	quiet := e.run("whoami")
	if quiet.Stderr != "" {
		t.Errorf("default output logs: %q", quiet.Stderr)
	}
	for name, loud := range map[string]result{
		"--verbose": e.run("--verbose", "whoami"),
		"env": func() result {
			t.Setenv("KRAVIS_LOG", "debug")
			defer t.Setenv("KRAVIS_LOG", "")
			return e.run("whoami")
		}(),
	} {
		if loud.Code != 0 || !strings.Contains(loud.Stderr, "level=DEBUG") || !strings.Contains(loud.Stderr, "workspace opened") {
			t.Errorf("%s: stderr %q", name, loud.Stderr)
		}
		if loud.Stdout != quiet.Stdout {
			t.Errorf("%s: logging changed stdout: %q", name, loud.Stdout)
		}
	}
	t.Setenv("KRAVIS_LOG", "info")
	if r := e.run("whoami"); r.Stderr != "" {
		t.Errorf("KRAVIS_LOG=info: %q", r.Stderr)
	}
}

func TestWORKSPACE_AC_11_CheckReportsAndNeverWrites(t *testing.T) {
	e := newEnv(t)
	e.mustInit()

	// A clean workspace: no findings, exit 0.
	before := e.Files()
	r := e.run("check")
	if r.Code != 0 || r.Stderr != "" || !strings.Contains(r.Stdout, `"data":[]`) {
		t.Fatalf("clean: code %d, %q %q", r.Code, r.Stdout, r.Stderr)
	}
	if !maps.Equal(before, e.Files()) {
		t.Fatal("check wrote on a clean workspace")
	}

	// Break it by hand.
	must(t, os.WriteFile("records/users/us_orphan.md", []byte("text"), 0o600))
	must(t, os.WriteFile("records/users/us_bad.json", []byte("{nope"), 0o600))
	must(t, os.WriteFile("records/users/us_noid.json", []byte(`{"kind":"human"}`), 0o600))
	must(t, os.WriteFile("config.json", []byte(`{"timezone":"Mars/Base","catalogs":{}}`), 0o600))
	must(t, os.Remove("kravis.json"))
	t.Setenv("KRAVIS_WORKSPACE", e.Workspace) // without kravis.json the folder is not found by itself
	locked := filepath.Join("records", "users", "us_locked.json")
	must(t, os.WriteFile(locked, []byte("{}"), 0o000))
	canRead := func() bool { _, err := os.ReadFile(locked); return err == nil }()

	before = e.Files()
	r = e.run("check")
	if r.Code != 6 || r.Stderr != "" {
		t.Fatalf("broken: code %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	byRecord := map[string]string{}
	var data []any
	data, _ = r.JSON(t)["data"].([]any)
	for _, f := range data {
		m := f.(map[string]any)
		if m["message"] == "" || m["code"] == "" {
			t.Errorf("incomplete finding %v", m)
		}
		byRecord[m["record"].(string)] = m["code"].(string)
	}
	want := map[string]string{
		"kravis.json":                "WORKSPACE_FILE_MISSING",
		"config.json":                "WORKSPACE_FILE_INVALID",
		"records/users/us_orphan.md": "ORPHAN_SIDECAR",
		"records/users/us_bad.json":  "MALFORMED_RECORD",
		"records/users/us_noid.json": "MALFORMED_RECORD",
	}
	if !canRead {
		want["records/users/us_locked.json"] = "UNREADABLE_RECORD"
	}
	for rec, code := range want {
		if byRecord[rec] != code {
			t.Errorf("%s: %q, want %s (all: %v)", rec, byRecord[rec], code, byRecord)
		}
	}
	if len(byRecord) != len(want) {
		t.Errorf("findings: %v", byRecord)
	}
	if !maps.Equal(before, e.Files()) {
		t.Error("check changed a file")
	}

	// An invalid kravis.json is a finding too, not a failure.
	must(t, os.WriteFile("kravis.json", []byte(`{"workspace":"ws_x","schema_version":9}`), 0o600))
	r = e.run("check")
	found := false
	for _, f := range r.JSON(t)["data"].([]any) {
		found = found || (f.(map[string]any)["record"] == "kravis.json" && f.(map[string]any)["code"] == "WORKSPACE_FILE_INVALID")
	}
	if r.Code != 6 || !found {
		t.Errorf("invalid kravis.json: %q", r.Stdout)
	}

	// A failure to look is an error, not a finding.
	e.Connector = errConnector{storage.NewMemory(), storage.ErrUnavailable}
	wantError(t, e.run("check"), "STORAGE_UNAVAILABLE", 5)
}

func TestInterruptedInitRefusesAnotherUser(t *testing.T) {
	e := newEnv(t)
	e.Connector = &failAt{Connector: storage.NewLocal(e.Workspace), sub: "records/users/"}
	wantError(t, e.init(), "INTERNAL", 1) // ana's init stops after the organization
	e.Connector = nil
	before := e.Files()
	// bo retries with the same organization: the organization is attributed to ana.
	r := e.run("init", "--timezone", tz, "--organization", orgName, "--user", "bo-acme")
	wantError(t, r, "WORKSPACE_EXISTS", 2)
	if !maps.Equal(before, e.Files()) {
		t.Error("files changed")
	}
}

// raceOn lets another init, with the same data, win the create of a path.
type raceOn struct {
	storage.Connector
	sub   string
	rival func()
	done  bool
}

func (c *raceOn) Create(p string, data []byte) (storage.Token, error) {
	if !c.done && strings.Contains(p, c.sub) {
		c.done = true
		c.rival()
	}
	return c.Connector.Create(p, data)
}

func TestConcurrentMatchingInitsAdoptEachOther(t *testing.T) {
	for _, sub := range []string{"config.json", "records/organizations/", "records/users/", "kravis.json"} {
		t.Run(sub, func(t *testing.T) {
			e := newEnv(t)
			local := storage.NewLocal(e.Workspace)
			e.Connector = &raceOn{Connector: local, sub: sub, rival: func() {
				_, err := domain.Init(local, nil, domain.InitInput{Timezone: tz, Organization: orgName, UserKey: userKey, UserName: userName})
				must(t, err)
			}}
			r := e.init()
			if r.Code != 0 {
				t.Fatalf("code %d, stderr %q", r.Code, r.Stderr)
			}
			if got := e.Files(); len(got) != 5 {
				t.Errorf("files: %v", got)
			}
			if r := e.init(); r.JSON(t)["outcome"] != "existing" {
				t.Errorf("rerun: %q", r.Stdout)
			}
		})
	}
	// A rival with other data is a conflict, not an internal error.
	e := newEnv(t)
	local := storage.NewLocal(e.Workspace)
	e.Connector = &raceOn{Connector: local, sub: "config.json", rival: func() {
		_, err := domain.Init(local, nil, domain.InitInput{Timezone: "UTC", Organization: orgName, UserKey: userKey})
		must(t, err)
	}}
	wantError(t, e.init(), "WORKSPACE_EXISTS", 2)
}

func TestCheckFlagsAnInvalidWorkspaceID(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	must(t, os.WriteFile("kravis.json", []byte(`{"workspace":"ws_x","schema_version":1}`), 0o600))
	r := e.run("check")
	if r.Code != 6 || !strings.Contains(r.Stdout, "WORKSPACE_FILE_INVALID") {
		t.Errorf("code %d, %q", r.Code, r.Stdout)
	}
}

// everyRace lets a rival with the same data win the create of every file.
type everyRace struct{ storage.Connector }

func (c everyRace) Create(p string, data []byte) (storage.Token, error) {
	if _, err := c.Connector.Create(p, data); err != nil {
		return "", err
	}
	return "", fmt.Errorf("create %s: %w", p, storage.ErrExists)
}

func TestInitAdoptsWhatSeveralRivalsWrote(t *testing.T) {
	e := newEnv(t)
	e.Connector = everyRace{storage.NewLocal(e.Workspace)}
	r := e.init()
	if r.Code != 0 {
		t.Fatalf("code %d, stderr %q", r.Code, r.Stderr)
	}
	if got := e.Files(); len(got) != 5 {
		t.Errorf("files: %v", got)
	}
}

func TestNoWorkspaceIsNotRetryable(t *testing.T) {
	e := newEnv(t)
	t.Chdir(t.TempDir()) // outside any workspace

	eo := wantError(t, e.run("whoami"), "STORAGE_UNAVAILABLE", 5)
	if eo["retryable"] != false {
		t.Errorf("no workspace: retryable = %v, want false", eo["retryable"])
	}

	// A workspace folder that is incomplete is a storage failure that may pass.
	t.Setenv("KRAVIS_WORKSPACE", t.TempDir())
	eo = wantError(t, e.run("whoami"), "STORAGE_UNAVAILABLE", 5)
	if eo["retryable"] != true {
		t.Errorf("incomplete workspace: retryable = %v, want true", eo["retryable"])
	}
}
