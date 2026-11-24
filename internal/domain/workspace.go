// Package domain holds Kravis's records and rules.
package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
	_ "time/tzdata" // zones resolve without a system database

	"go.jetify.com/typeid"

	"github.com/diegomarino/kravis/internal/storage"
)

// Paths of the workspace's own files, relative to its folder.
const (
	MarkerPath = "kravis.json"
	ConfigPath = "config.json"
	// SchemaVersion is the storage format this build reads and writes.
	SchemaVersion = 1
)

// Marker is kravis.json: the workspace ID and the schema version. It is
// written last by init, so its presence means the workspace is complete.
type Marker struct {
	Workspace     string `json:"workspace"`
	SchemaVersion int    `json:"schema_version"`
}

// Status is one entry of the task status catalog.
type Status struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// Catalogs are the lists a workspace configures for itself.
type Catalogs struct {
	TaskStatuses []Status `json:"task_statuses"`
	Purposes     []string `json:"purposes"`
	Channels     []string `json:"channels"`
	PartyRoles   []string `json:"party_roles"`
	LostReasons  []string `json:"lost_reasons"`
}

// Config is config.json: the timezone and the catalogs.
type Config struct {
	Timezone string   `json:"timezone"`
	Catalogs Catalogs `json:"catalogs"`
}

// forHolder is the configuration that applies to a holder. In the MVP it is
// the workspace's own, so per-holder configuration can be added later
// without touching the callers (ADR-001).
func (c Config) forHolder(string) Config { return c }

// ConfigFor returns the configuration for holder H: every catalog lookup
// goes through it.
func (ws *Workspace) ConfigFor(holder string) Config { return ws.Config.forHolder(holder) }

var statusCategories = []string{"open", "waiting", "done", "canceled"}

// DefaultCatalogs are the catalogs init writes.
func DefaultCatalogs() Catalogs {
	return Catalogs{
		TaskStatuses: []Status{
			{"todo", "open"}, {"in-progress", "open"}, {"waiting", "waiting"},
			{"done", "done"}, {"canceled", "canceled"},
		},
		Purposes:    []string{"follow-up", "ping", "expand", "intro", "prepare"},
		Channels:    []string{"email", "meeting", "call", "message"},
		PartyRoles:  []string{"client", "counterparty", "advisor", "lender", "investor", "buyer", "seller"},
		LostReasons: []string{"price", "timing", "competitor", "no-decision", "ghosting", "fit", "other"},
	}
}

// Validate reports the first thing wrong with a configuration.
func (c Config) Validate() error {
	if err := ValidTimezone(c.Timezone); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range c.Catalogs.TaskStatuses {
		if s.Name == "" || seen[s.Name] {
			return fmt.Errorf("task status %q is empty or repeated", s.Name)
		}
		seen[s.Name] = true
		if !slices.Contains(statusCategories, s.Category) {
			return fmt.Errorf("task status %q has category %q, not one of %v", s.Name, s.Category, statusCategories)
		}
	}
	return nil
}

// ValidTimezone accepts an IANA zone name such as Europe/Madrid.
func ValidTimezone(name string) error {
	if name == "" || name == "Local" {
		return fmt.Errorf("%q is not an IANA timezone", name)
	}
	if _, err := time.LoadLocation(name); err != nil {
		return fmt.Errorf("%q is not an IANA timezone", name)
	}
	return nil
}

// Validate reports the first thing wrong with the marker.
func (m Marker) Validate() error {
	if id, err := typeid.FromString(m.Workspace); err != nil || id.Prefix() != "ws" {
		return fmt.Errorf("workspace %q is not a workspace ID (ws_<typeid>)", m.Workspace)
	}
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version %d is not %d", m.SchemaVersion, SchemaVersion)
	}
	return nil
}

// encodeFile writes a workspace file: two-space indentation, UTF-8
// unescaped, a trailing newline.
func encodeFile(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// errInvalidFile marks a workspace file that exists but is not valid.
var errInvalidFile = errors.New("invalid workspace file")

// readFile reads and checks a workspace file. A missing file is
// storage.ErrNotFound; an invalid one wraps errInvalidFile.
func readFile[T interface{ Validate() error }](c storage.Connector, path string, v *T) error {
	data, _, err := c.Get(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w: %w", path, errInvalidFile, err)
	}
	if err := (*v).Validate(); err != nil {
		return fmt.Errorf("%s: %w: %w", path, errInvalidFile, err)
	}
	return nil
}

// Workspace is an opened workspace.
type Workspace struct {
	Marker Marker
	Config Config
	Store  *storage.Store
	now    func() time.Time
}

// clock is the time of the call, for what depends on today's date.
func (ws *Workspace) clock() time.Time { return ws.now() }

// ErrNoWorkspace is the cause of NoWorkspace: waiting never fixes it, so the
// CLI reports it as not retryable.
var ErrNoWorkspace = errors.New("no workspace found")

// NoWorkspace is the error of a command run outside any workspace.
func NoWorkspace() *Error {
	e := newError(CodeStorageUnavailable,
		"run kravis init in a new folder, or set KRAVIS_WORKSPACE to an existing workspace",
		"no workspace found: there is no kravis.json in this folder or any folder above it")
	e.Cause = ErrNoWorkspace
	return e
}

// Open reads kravis.json and config.json of the workspace on c. now gives
// the time of each write; nil means time.Now.
func Open(c storage.Connector, now func() time.Time) (*Workspace, error) {
	if now == nil {
		now = time.Now
	}
	ws := &Workspace{Store: storage.NewStore(c, now), now: now}
	if err := readFile(c, MarkerPath, &ws.Marker); err != nil {
		return nil, openError(err)
	}
	if err := readFile(c, ConfigPath, &ws.Config); err != nil {
		return nil, openError(err)
	}
	return ws, nil
}

func openError(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return newError(CodeStorageUnavailable,
			"run kravis init, or kravis check to see what is missing",
			"the workspace is incomplete or missing: %v", err)
	case errors.Is(err, errInvalidFile):
		return newError(CodeStorageUnavailable, "run kravis check to see what is wrong", "%v", err)
	}
	return err
}
