package storage

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// connectors runs the same contract against every connector.
func connectors(t *testing.T) map[string]Connector {
	return map[string]Connector{
		"local":  NewLocal(t.TempDir()),
		"memory": NewMemory(),
	}
}

func TestConnectorContract(t *testing.T) {
	for name, c := range connectors(t) {
		t.Run(name, func(t *testing.T) {
			const p = "records/people/pe_ana.json"
			if _, _, err := c.Get(p); !errors.Is(err, ErrNotFound) {
				t.Fatalf("get of a missing object: %v", err)
			}
			if err := c.Create(p, []byte("one")); err != nil {
				t.Fatal(err)
			}
			if err := c.Create(p, []byte("two")); !errors.Is(err, ErrExists) {
				t.Fatalf("create over an object: %v", err)
			}
			data, tok, err := c.Get(p)
			if err != nil || string(data) != "one" || tok == "" {
				t.Fatalf("get = %q, %q, %v", data, tok, err)
			}

			if err := c.Replace(p, []byte("two"), tok); err != nil {
				t.Fatal(err)
			}
			if err := c.Replace(p, []byte("three"), tok); !errors.Is(err, ErrTokenMismatch) {
				t.Fatalf("replace with a stale token: %v", err)
			}
			if err := c.Delete(p, tok); !errors.Is(err, ErrTokenMismatch) {
				t.Fatalf("delete with a stale token: %v", err)
			}
			data, tok2, _ := c.Get(p)
			if string(data) != "two" || tok2 == tok {
				t.Fatalf("after replace: %q, token %q (was %q)", data, tok2, tok)
			}
			if err := c.Replace("records/people/nope.json", nil, tok2); !errors.Is(err, ErrNotFound) {
				t.Fatalf("replace of a missing object: %v", err)
			}
			if err := c.Delete("records/people/nope.json", tok2); !errors.Is(err, ErrNotFound) {
				t.Fatalf("delete of a missing object: %v", err)
			}

			_ = c.Create("records/people/pe_ben.json", []byte("b"))
			_ = c.Create("records/organizations/org_x.json", []byte("x"))
			_ = c.Create("keys/person-email/ab/abc.json", []byte("k"))
			_ = c.Create("records/users/us_test.tmp-agent.json", []byte("u")) // only a leading dot marks a temporary file
			for prefix, want := range map[string][]string{
				"records/people/":     {"records/people/pe_ana.json", "records/people/pe_ben.json"},
				"records/people/pe_b": {"records/people/pe_ben.json"},
				"records/":            {"records/organizations/org_x.json", "records/people/pe_ana.json", "records/people/pe_ben.json", "records/users/us_test.tmp-agent.json"},
				"":                    {"keys/person-email/ab/abc.json", "records/organizations/org_x.json", "records/people/pe_ana.json", "records/people/pe_ben.json", "records/users/us_test.tmp-agent.json"},
				"records/users/":      {"records/users/us_test.tmp-agent.json"},
				"records/tasks/":      nil,
				"nowhere/at/all":      nil,
			} {
				got, err := c.List(prefix)
				if err != nil || !slices.Equal(got, want) {
					t.Errorf("list %q = %v, %v; want %v", prefix, got, err, want)
				}
			}

			if err := c.Delete(p, tok2); err != nil {
				t.Fatal(err)
			}
			if _, _, err := c.Get(p); !errors.Is(err, ErrNotFound) {
				t.Fatalf("get after delete: %v", err)
			}
		})
	}
}

func TestLocalRefusesPathsOutsideTheWorkspace(t *testing.T) {
	l := NewLocal(t.TempDir())
	for _, p := range []string{"", ".", "/etc/passwd", "../x.json", "a/../../x.json", "a//b.json", `..\x.json`, `a\..\..\x.json`, "C:x.json", "c:/x.json"} {
		if _, _, err := l.Get(p); err == nil {
			t.Errorf("get %q: want an error", p)
		}
		if err := l.Create(p, nil); err == nil {
			t.Errorf("create %q: want an error", p)
		}
		if err := l.Replace(p, nil, ""); err == nil {
			t.Errorf("replace %q: want an error", p)
		}
		if err := l.Delete(p, ""); err == nil {
			t.Errorf("delete %q: want an error", p)
		}
	}
	if _, err := l.List("../"); err == nil {
		t.Error("list ../: want an error")
	}
}

func TestLocalTokenChangesOnEveryWrite(t *testing.T) {
	l := NewLocal(t.TempDir())
	const p = "records/x.json"
	_ = l.Create(p, []byte("same"))
	_, before, _ := l.Get(p)
	if err := l.Replace(p, []byte("same"), before); err != nil {
		t.Fatal(err)
	}
	if _, after, _ := l.Get(p); after == before {
		t.Fatal("rewriting the same bytes must give a new token")
	}
}

func TestLocalLeavesNoTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	l := NewLocal(root)
	const p = "records/x.json"
	_ = l.Create(p, []byte("a"))
	_ = l.Create(p, []byte("b")) // refused
	_, tok, _ := l.Get(p)
	_ = l.Replace(p, []byte("c"), "stale") // refused
	_ = l.Replace(p, []byte("d"), tok)
	entries, _ := os.ReadDir(filepath.Join(root, "records"))
	if len(entries) != 1 || entries[0].Name() != "x.json" {
		t.Fatalf("records/ holds %v", entries)
	}
	// A temporary file left by a crash is never listed.
	_ = os.WriteFile(filepath.Join(root, "records", ".x.json.tmp-123"), nil, 0o644)
	if got, _ := l.List("records/"); !slices.Equal(got, []string{p}) {
		t.Fatalf("list = %v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "records", "x.json")); string(data) != "d" {
		t.Fatalf("content = %q", data)
	}
}

func TestLocalUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("folder permissions do not apply")
	}
	root := filepath.Join(t.TempDir(), "ws")
	if err := os.WriteFile(root, []byte("a file, not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLocal(root)
	if _, _, err := l.Get("x.json"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("get: %v", err)
	}
	if err := l.Create("x.json", nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("create: %v", err)
	}
	if err := l.Replace("x.json", nil, ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("replace: %v", err)
	}
	if err := l.Delete("x.json", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("delete: %v", err)
	}
	if _, err := l.List("records/"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("list: %v", err)
	}
}

func TestMemoryHookFailsACall(t *testing.T) {
	m := NewMemory()
	boom := errors.New("boom")
	m.Hook = func(op Op, _ string) error {
		if op == OpList {
			return boom
		}
		return nil
	}
	if _, err := m.List(""); !errors.Is(err, boom) {
		t.Fatalf("list: %v", err)
	}
	m.Hook = func(Op, string) error { return boom }
	if _, _, err := m.Get("a"); !errors.Is(err, boom) {
		t.Error("get")
	}
	if err := m.Create("a", nil); !errors.Is(err, boom) {
		t.Error("create")
	}
	if err := m.Replace("a", nil, ""); !errors.Is(err, boom) {
		t.Error("replace")
	}
	if err := m.Delete("a", ""); !errors.Is(err, boom) {
		t.Error("delete")
	}
	if len(m.Snapshot()) != 0 {
		t.Error("a failed call wrote something")
	}
}
