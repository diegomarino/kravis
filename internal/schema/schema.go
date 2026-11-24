// Package schema generates kravis schema: a JSON Schema for the input of
// each record type, generated from the annotated Go types of the domain,
// with the workspace's catalogs injected at run time.
package schema

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/invopop/jsonschema"

	"github.com/diegomarino/kravis/internal/domain"
)

// Extension is the key of what the standard keywords cannot say: defaults
// when a field is omitted, the list order and view, statuses with their
// categories and the error codes.
const Extension = "x-kravis"

// Names lists the record types that have a schema.
func Names() []string {
	var out []string
	for _, t := range domain.Types() {
		out = append(out, t.Name)
	}
	return out
}

// For returns the schema of a record type for the configuration of a
// holder. An unknown type is an INVALID_VALUE that lists the known ones.
func For(name string, cfg domain.Config) (*jsonschema.Schema, error) {
	var typ *domain.Type
	for _, t := range domain.Types() {
		if t.Name == name {
			typ = &t
		}
	}
	input := domain.InputFor(name)
	if typ == nil || input == nil {
		return nil, domain.InvalidFlag("type", Names(), "%q is not a record type", name)
	}
	r := &jsonschema.Reflector{ExpandedStruct: true, DoNotReference: true}
	s := r.Reflect(input)
	s.ID = ""
	s.Version = "https://json-schema.org/draft/2020-12/schema"
	s.Title = name
	s.Description = fmt.Sprintf("The fields of kravis %s create.", name)
	s.AdditionalProperties = jsonschema.FalseSchema

	cat := cfg.Catalogs
	switch name {
	case "user":
		enum(s, "kind", domain.Kinds())
	case "person":
		minItems(s, "emails", 1)
		items(s, "emails", &jsonschema.Schema{Type: "string", Pattern: emailPattern})
		s.AnyOf = []*jsonschema.Schema{{Required: []string{"name"}}, {Required: []string{"emails"}}}
		s.Required = nil
	case "organization":
		items(s, "domains", &jsonschema.Schema{Type: "string", Pattern: domainPattern,
			Not: &jsonschema.Schema{Pattern: providerPattern()}})
	case "interaction":
		enum(s, "channel", cat.Channels)
		enum(s, "direction", domain.Directions())
	case "task":
		enum(s, "purpose", cat.Purposes)
	}

	ext := map[string]any{
		"type":     name,
		"list":     map[string]any{"order": typ.Order, "compact": typ.Compact, "default_limit": domain.DefaultLimit},
		"errors":   domain.ErrorCodes(),
		"defaults": domain.Defaults(name),
	}
	if name == "task" {
		ext["statuses"] = cat.TaskStatuses
	}
	s.Extras = map[string]any{Extension: ext}
	return s, nil
}

// The shapes of an organization's domain (lowercase labels) and of an email.
const (
	domainPattern = `^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`
	emailPattern  = `^[^@\s]+@[^@\s]+\.[^@\s]+$`
)

// providerPattern matches the email providers, which are never an
// organization's domain, in any case: Kravis lowercases a domain before it
// compares it, and JSON Schema patterns have no case-insensitive flag.
func providerPattern() string {
	var alts []string
	for _, d := range domain.ProviderDomains() {
		var b strings.Builder
		for _, r := range d {
			if u := unicode.ToUpper(r); u != r {
				fmt.Fprintf(&b, "[%c%c]", r, u)
			} else {
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
		alts = append(alts, b.String())
	}
	return "^(" + strings.Join(alts, "|") + ")$"
}

func enum(s *jsonschema.Schema, field string, values []string) {
	if p, ok := s.Properties.Get(field); ok && len(values) > 0 {
		p.Enum = nil
		for _, v := range values {
			p.Enum = append(p.Enum, v)
		}
	}
}

func minItems(s *jsonschema.Schema, field string, n uint64) {
	if p, ok := s.Properties.Get(field); ok {
		p.MinItems = &n
	}
}

func items(s *jsonschema.Schema, field string, item *jsonschema.Schema) {
	if p, ok := s.Properties.Get(field); ok {
		p.Items = item
	}
}
