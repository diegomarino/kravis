// Package specgate classifies a pull request against the spec rules of
// docs/development.md (Autonomy): additive changes merge on their own, the
// rest wait for the person's spec-approved label.
package specgate

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

type modelDiff struct {
	Removed   []record  `json:"removed"`
	Relocated []record  `json:"relocated"`
	Changed   []changed `json:"changed"`
}

type record struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type changed struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Changes []change `json:"changes"`
}

type change struct {
	Path          string `json:"path"`
	BeforePresent bool   `json:"beforePresent"`
	AfterPresent  bool   `json:"afterPresent"`
	Before        any    `json:"before"`
	After         any    `json:"after"`
}

// ClassifyModelDiff reads the JSON of `ddduck diff --json` and returns why
// the change needs the person; none means it is additive. Added records are
// always fine. A changed concept, use case or domain is fine when it only
// adds text or list items; any change to a guarantee or relationship is not.
func ClassifyModelDiff(doc []byte) ([]string, error) {
	var d modelDiff
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil, fmt.Errorf("reading the ddduck diff: %w", err)
	}
	var reasons []string
	for _, r := range d.Removed {
		reasons = append(reasons, fmt.Sprintf("model: %s %s was removed", r.Kind, r.ID))
	}
	for _, r := range d.Relocated {
		reasons = append(reasons, fmt.Sprintf("model: %s %s was relocated", r.Kind, r.ID))
	}
	for _, c := range d.Changed {
		switch c.Kind {
		case "Guarantee", "Relationship":
			reasons = append(reasons, fmt.Sprintf("model: %s %s was changed", c.Kind, c.ID))
			continue
		}
		for _, ch := range c.Changes {
			if !additive(ch) {
				reasons = append(reasons, fmt.Sprintf("model: %s %s lost or changed text at %s", c.Kind, c.ID, ch.Path))
			}
		}
	}
	return reasons, nil
}

func additive(c change) bool {
	if !c.BeforePresent {
		return true
	}
	if !c.AfterPresent {
		return false
	}
	switch before := c.Before.(type) {
	case string:
		after, ok := c.After.(string)
		return ok && strings.Contains(after, before)
	case []any:
		after, ok := c.After.([]any)
		return ok && containsAll(after, before)
	}
	return reflect.DeepEqual(c.Before, c.After)
}

func containsAll(after, before []any) bool {
	for _, b := range before {
		found := false
		for _, a := range after {
			if reflect.DeepEqual(a, b) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ClassifyLineDiff reads a `git diff -U0` of the guarded documents
// (GLOSSARY.md, docs/cli.md, docs/storage.md): they may gain lines but not
// lose or change one.
func ClassifyLineDiff(unified string) []string {
	var reasons []string
	file := ""
	for _, line := range strings.Split(unified, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			file = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "--- a/"), strings.HasPrefix(line, "--- /dev/null"):
			// a file header, not a removed line
		case strings.HasPrefix(line, "-"):
			reasons = append(reasons, removedLine(file, line))
		}
	}
	return reasons
}

func removedLine(file, line string) string {
	return fmt.Sprintf("%s lost a line: %s", file, strings.TrimSpace(strings.TrimPrefix(line, "-")))
}

// ClassifyNameStatus reads `git diff --name-status` limited to
// docs/ddd/decisions/: any added, changed or removed decision needs the person.
func ClassifyNameStatus(nameStatus string) []string {
	var reasons []string
	for _, line := range strings.Split(nameStatus, "\n") {
		if strings.TrimSpace(line) != "" {
			reasons = append(reasons, "decision changed: "+strings.Join(strings.Fields(line), " "))
		}
	}
	return reasons
}

// ClassifyContractFiles reads `git diff --name-status` limited to the
// contract paths: an added file is additive, any other change needs the
// person.
func ClassifyContractFiles(nameStatus string) []string {
	var reasons []string
	for _, line := range strings.Split(nameStatus, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] != "A" {
			reasons = append(reasons, "contract file changed: "+strings.Join(fields, " "))
		}
	}
	return reasons
}

var breaking = regexp.MustCompile(`^[a-z]+(\([^)]*\))?!:`)

// ClassifySubjects flags every commit subject that carries `!`.
func ClassifySubjects(subjects []string) []string {
	var reasons []string
	for _, s := range subjects {
		if breaking.MatchString(s) {
			reasons = append(reasons, "breaking commit: "+s)
		}
	}
	return reasons
}
