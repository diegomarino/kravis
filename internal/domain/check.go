package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/diegomarino/kravis/internal/storage"
)

// Finding codes of `kravis check`.
const (
	FindingFileMissing      = "WORKSPACE_FILE_MISSING"
	FindingFileInvalid      = "WORKSPACE_FILE_INVALID"
	FindingOrphanSidecar    = "ORPHAN_SIDECAR"
	FindingUnreadableRecord = "UNREADABLE_RECORD"
	FindingMalformedRecord  = "MALFORMED_RECORD"
	FindingInvalidRecord    = "INVALID_RECORD"
	FindingKeyWithoutValue  = "KEY_WITHOUT_VALUE"
	FindingValueWithoutKey  = "VALUE_WITHOUT_KEY"
)

// audits check the records of each type against its rules, and its
// natural keys against the values they stand for.
var audits = []func(st *storage.Store, add addFinding) error{people.audit, organizations.audit}

type addFinding func(code, record, format string, args ...any)

// Finding is one thing `check` found wrong. Record is the path of the file
// it concerns, relative to the workspace.
type Finding struct {
	Code    string `json:"code"`
	Record  string `json:"record"`
	Message string `json:"message"`
}

// anyRecord decodes any record far enough to see that it is one.
type anyRecord struct{ storage.Base }

// Check validates the workspace on c and returns what it finds, an empty
// (never nil) list for a clean workspace. It never writes. An error is a
// failure to look, not a finding.
func Check(c storage.Connector) ([]Finding, error) {
	findings := []Finding{}
	add := func(code, record, format string, args ...any) {
		findings = append(findings, Finding{code, record, fmt.Sprintf(format, args...)})
	}
	files := []struct {
		path string
		read func() error
	}{
		{MarkerPath, func() error { return readFile(c, MarkerPath, new(Marker)) }},
		{ConfigPath, func() error { return readFile(c, ConfigPath, new(Config)) }},
	}
	for _, f := range files {
		switch err := f.read(); {
		case err == nil:
		case errors.Is(err, storage.ErrNotFound):
			add(FindingFileMissing, f.path, "%s does not exist", f.path)
		case errors.Is(err, storage.ErrUnavailable):
			return nil, err
		default:
			add(FindingFileInvalid, f.path, "%v", err)
		}
	}

	paths, err := c.List("records/")
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		switch {
		case strings.HasSuffix(p, ".json"):
			data, _, err := c.Get(p)
			if errors.Is(err, storage.ErrNotFound) {
				continue // removed while we looked
			}
			if err != nil {
				add(FindingUnreadableRecord, p, "%v", err)
				continue
			}
			var rec anyRecord
			if err := storage.Decode(data, &rec); err != nil {
				add(FindingMalformedRecord, p, "not a valid record: %v", err)
			} else if rec.ID == "" {
				add(FindingMalformedRecord, p, "the record has no id")
			}
		case strings.HasSuffix(p, ".md"):
			if json := strings.TrimSuffix(p, ".md") + ".json"; !slices.Contains(paths, json) {
				add(FindingOrphanSidecar, p, "the sidecar has no JSON record next to it")
			}
		}
	}
	st := storage.NewStore(c, nil)
	for _, audit := range audits {
		if err := audit(st, add); err != nil {
			return nil, err
		}
	}
	return findings, nil
}
