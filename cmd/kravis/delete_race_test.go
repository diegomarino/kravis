package main

import (
	"errors"
	"testing"

	"github.com/diegomarino/kravis/internal/storage"
)

// hooked runs hook before each call of the connector it wraps, as Memory's
// Hook does, so the local connector can be interleaved too.
type hooked struct {
	storage.Connector
	hook func(path string)
}

func (h *hooked) Get(p string) ([]byte, storage.Token, error) {
	h.hook(p)
	return h.Connector.Get(p)
}

func (h *hooked) Replace(p string, data []byte, token storage.Token) (storage.Token, error) {
	h.hook(p)
	return h.Connector.Replace(p, data, token)
}

func (h *hooked) Delete(p string, token storage.Token) error {
	h.hook(p)
	return h.Connector.Delete(p, token)
}

// A delete whose JSON is gone must not clean up what a re-create of the same
// ID wrote in between: its summary and its deduplication key stay, and
// check finds nothing.
func TestAuditCLIDeleteRecreateSummary(t *testing.T) {
	for _, name := range []string{"memory", "local"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			t.Setenv("KRAVIS_WORKSPACE", e.Workspace)
			var inner storage.Connector = storage.NewMemory()
			if name == "local" {
				inner = storage.NewLocal(e.Workspace)
			}
			h := &hooked{Connector: inner, hook: func(string) {}}
			e.Connector = h
			e.mustInit()
			e.ok("person", "create", "--name", "Elena Ruiz")
			args := record("Data room documentation", "--ref", "email:message:msg-9@example.com", "--summary-file", "-")
			outcomeOf(t, e.runOK("old", args...), "created")

			done := false
			h.hook = func(p string) {
				if done || (p != sidecar(dataRoom) && p[:5] != "keys/") {
					return
				}
				if _, _, err := inner.Get(dataRoom); !errors.Is(err, storage.ErrNotFound) {
					return // the JSON is still there: not yet the window
				}
				done = true
				outcomeOf(t, e.runOK("new committed summary", args...), "created")
			}
			outcomeOf(t, e.ok("interaction", "delete", dataRoomID), "deleted")
			h.hook = func(string) {}

			if !done {
				t.Fatal("the delete never cleaned up after its JSON was gone")
			}
			if d := data(t, e.ok("interaction", "get", dataRoomID, "--full")); d["summary"] != "new committed summary" {
				t.Errorf("the re-create lost its summary: %v", d["summary"])
			}
			if f := e.findings(); len(f) != 0 {
				t.Errorf("check after the race: %v", f)
			}
		})
	}
}
