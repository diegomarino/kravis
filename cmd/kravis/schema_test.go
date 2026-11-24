package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var update = flag.Bool("update", false, "rewrite the golden schema files")

var schemaTypes = []string{"interaction", "organization", "person", "task", "user"}

// schemaOf runs kravis schema and returns its data.
func schemaOf(t *testing.T, e *env, args ...string) map[string]any {
	t.Helper()
	return data(t, e.ok(append([]string{"schema"}, args...)...))
}

func TestWORKSPACE_AC_06_SchemaDescribesATypeWithItsCatalogsDefaultsOrderAndErrors(t *testing.T) {
	e := taskWorld(t)
	e.setChannels("email", "visit")

	s := schemaOf(t, e, "task")
	ext := obj(t, s, "x-kravis")
	// The category of every status.
	got := fmt.Sprint(ext["statuses"])
	want := "[map[category:open name:todo] map[category:open name:in-progress] map[category:waiting name:waiting] map[category:done name:done] map[category:canceled name:canceled]]"
	if got != want {
		t.Errorf("statuses %s", got)
	}
	// The value used when a field is omitted.
	if d := obj(t, ext, "defaults"); d["status"] != "todo" || fmt.Sprint(d["purpose"]) != "map[value:follow-up when:interaction]" {
		t.Errorf("defaults %v", d)
	}
	// The default order and the compact view, as the list code declares them.
	list := obj(t, ext, "list")
	if !strings.HasPrefix(list["order"].(string), "due, soonest first") ||
		fmt.Sprint(list["compact"]) != "[id title live holder status category assignee agent due]" {
		t.Errorf("list %v", list)
	}
	// The possible error codes: every code the CLI can exit with.
	var codes []string
	for _, c := range ext["errors"].([]any) {
		codes = append(codes, c.(string))
	}
	var exits []string
	for c := range exitCodes {
		exits = append(exits, c)
	}
	slices.Sort(codes)
	slices.Sort(exits)
	if diff := cmp.Diff(exits, codes); diff != "" {
		t.Errorf("error codes differ from the exit codes: %s", diff)
	}
	// The catalog values of the holder, as enumerations.
	purpose := obj(t, obj(t, s, "properties"), "purpose")
	if fmt.Sprint(purpose["enum"]) != "[follow-up ping expand intro prepare]" {
		t.Errorf("purpose %v", purpose)
	}
	channel := obj(t, obj(t, schemaOf(t, e, "interaction"), "properties"), "channel")
	if fmt.Sprint(channel["enum"]) != "[email visit]" {
		t.Errorf("a configured catalog shows up in the schema: %v", channel)
	}
	// Without a type, every type; an unknown one lists them.
	all := schemaOf(t, e)
	if len(all) != len(schemaTypes) {
		t.Errorf("types %d", len(all))
	}
	eo := wantError(t, e.run("schema", "deal"), "INVALID_VALUE", 2)
	allowed := slices.Clone(obj(t, eo, "details")["allowed"].([]any))
	slices.SortFunc(allowed, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
	if fmt.Sprint(allowed) != fmt.Sprint(anySlice(schemaTypes)) {
		t.Errorf("allowed %v", eo)
	}
	// A schema is JSON also on a terminal, and needs a workspace.
	e.TTY = true
	if r := e.run("schema", "task"); !strings.HasPrefix(r.Stdout, `{"data":`) {
		t.Errorf("on a TTY: %q", r.Stdout)
	}
	e.TTY = false
	t.Chdir(t.TempDir())
	wantError(t, e.run("schema", "task"), "STORAGE_UNAVAILABLE", 5)
}

func anySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// TestSchemaGolden keeps the contract: kravis schema <type> equals its
// golden file. A changed or removed golden needs the spec-approved label; go
// test ./cmd/kravis -run TestSchemaGolden -update rewrites them.
func TestSchemaGolden(t *testing.T) {
	e := newEnv(t)
	e.mustInit()
	for _, name := range schemaTypes {
		t.Run(name, func(t *testing.T) {
			r := e.run("schema", name)
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, []byte(strings.TrimSpace(r.Stdout)), "", "  "); err != nil {
				t.Fatalf("%q: %v", r.Stdout, err)
			}
			pretty.WriteByte('\n')
			path := filepath.Join(goldenDir, name+".json")
			if *update {
				must(t, os.MkdirAll(goldenDir, 0o755))
				must(t, os.WriteFile(path, pretty.Bytes(), 0o644))
			}
			want, err := os.ReadFile(path)
			must(t, err)
			if diff := cmp.Diff(string(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))), pretty.String()); diff != "" {
				t.Errorf("kravis schema %s differs from %s (-golden +output); run with -update if intended:\n%s", name, path, diff)
			}
		})
	}
}

var goldenDir = filepath.Join(testdataDir(), "schema")

func testdataDir() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "testdata")
}

// --- Schema matches validation -----------------------------------------

// A case is an input document and whether Kravis must accept it. The corpus
// is written by hand, independently of the generated schema. It holds only
// single-record rules: whether the assignee exists, whether a handler is a
// human or whether an agent has one are rules over other records or fields,
// tested through the CLI and not in the schema. Kravis lowercases and trims
// emails and domains before it judges them, so the corpus has no variants
// that only differ in case.
type inputCase struct {
	name string
	doc  string
	ok   bool
}

var corpus = map[string][]inputCase{
	"task": {
		{"title and assignee", `{"title":"Send the deck","assignee":"us_ana-acme"}`, true},
		{"with a due date", `{"title":"Call back","assignee":"us_ana-acme","due":"2026-12-01"}`, true},
		{"with a purpose of the catalog", `{"title":"Ping Elena","assignee":"us_ana-acme","purpose":"ping"}`, true},
		{"with the assignee's agent", `{"title":"Draft the reply","assignee":"us_ana-acme","agent":"us_mail-acme"}`, true},
		{"with a description", `{"title":"Read the contract","assignee":"us_ana-acme","description":"Pages 3 to 9."}`, true},
		{"no title", `{"assignee":"us_ana-acme"}`, false},
		{"no assignee", `{"title":"Orphan"}`, false},
		{"a blank title", `{"title":"   ","assignee":"us_ana-acme"}`, false},
		{"an assignee that is not a user ID", `{"title":"Bad assignee","assignee":"ana"}`, false},
		{"a date that does not exist", `{"title":"Bad date","assignee":"us_ana-acme","due":"2026-02-30"}`, false},
		{"a purpose outside the catalog", `{"title":"Bad purpose","assignee":"us_ana-acme","purpose":"nag"}`, false},
		{"an unknown field", `{"title":"Owned","assignee":"us_ana-acme","owner":"us_ana-acme"}`, false},
	},
	"interaction": {
		{"the required fields", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","subject":"Data room","participants":["pe_elena-ruiz"]}`, true},
		{"with an offset", `{"channel":"meeting","occurred_at":"2026-11-23T18:02:00+01:00","subject":"Kickoff","participants":["pe_elena-ruiz","org_grupo-sierra"]}`, true},
		{"inbound with its initiator", `{"channel":"call","occurred_at":"2026-11-22T10:00:00Z","subject":"Call from Elena","participants":["pe_elena-ruiz"],"direction":"inbound","initiated_by":"pe_elena-ruiz"}`, true},
		{"with a reference", `{"channel":"email","occurred_at":"2026-11-21T09:00:00Z","subject":"Referenced","participants":["pe_elena-ruiz"],"refs":["email:message:a1@example.com"]}`, true},
		{"with a summary", `{"channel":"email","occurred_at":"2026-11-20T09:00:00Z","subject":"Summarized","participants":["pe_elena-ruiz"],"summary":"Agreed the next step."}`, true},
		{"a scoped reference", `{"channel":"email","occurred_at":"2026-11-19T09:00:00Z","subject":"Scoped","participants":["pe_elena-ruiz"],"refs":["gmail:message:18f3a2@ana@example.com"]}`, true},
		{"a reference that is not one", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","subject":"x","participants":["pe_elena-ruiz"],"refs":["nonsense"]}`, false},
		{"a Gmail reference without its account", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","subject":"x","participants":["pe_elena-ruiz"],"refs":["gmail:message:18f3a2"]}`, false},
		{"no channel", `{"occurred_at":"2026-11-23T17:02:00Z","subject":"x","participants":["pe_elena-ruiz"]}`, false},
		{"no date", `{"channel":"email","subject":"x","participants":["pe_elena-ruiz"]}`, false},
		{"no subject", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","participants":["pe_elena-ruiz"]}`, false},
		{"no participants", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","subject":"x"}`, false},
		{"an empty list of participants", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","subject":"x","participants":[]}`, false},
		{"a channel outside the catalog", `{"channel":"fax","occurred_at":"2026-11-23T17:02:00Z","subject":"x","participants":["pe_elena-ruiz"]}`, false},
		{"a date without a time", `{"channel":"email","occurred_at":"2026-11-23","subject":"x","participants":["pe_elena-ruiz"]}`, false},
		{"a direction that does not exist", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","subject":"x","participants":["pe_elena-ruiz"],"direction":"sideways"}`, false},
		{"a blank subject", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","subject":" ","participants":["pe_elena-ruiz"]}`, false},
		{"an unknown field", `{"channel":"email","occurred_at":"2026-11-23T17:02:00Z","subject":"x","participants":["pe_elena-ruiz"],"deal":"d1"}`, false},
	},
	"person": {
		{"a name", `{"name":"Zoe Example"}`, true},
		{"an email", `{"emails":["zoe@example.com"]}`, true},
		{"both", `{"name":"Zoe Two","emails":["zoe.two@example.com","zoe2@example.net"]}`, true},
		{"nothing", `{}`, false},
		{"a blank name", `{"name":"  "}`, false},
		{"an email that is not one", `{"emails":["zoe"]}`, false},
		{"an empty list of emails", `{"emails":[]}`, false},
		{"a name and no emails listed", `{"name":"Zoe Four","emails":[]}`, true},
		{"an empty name beside an email", `{"name":"","emails":["zoe4@example.com"]}`, true},
		{"an email with spaces around it", `{"emails":[" zoe5@example.com "]}`, true},
		{"an unknown field", `{"name":"Zoe Three","phone":"123"}`, false},
	},
	"organization": {
		{"a name", `{"name":"Zeta Example"}`, true},
		{"an own organization", `{"name":"Eta Example","own":true}`, true},
		{"with a domain", `{"name":"Theta Example","domains":["theta.example.com"]}`, true},
		{"a domain in capitals", `{"name":"Iota Example","domains":["IOTA.example.com"]}`, true},
		{"a domain with spaces around it", `{"name":"Iota Two","domains":[" iota2.example.com "]}`, true},
		{"no name", `{"domains":["kappa.example.com"]}`, false},
		{"a blank name", `{"name":"  "}`, false},
		{"a domain that is not one", `{"name":"Lambda Example","domains":["not a domain"]}`, false},
		{"an email provider's domain", `{"name":"Mu Example","domains":["gmail.com"]}`, false},
		{"an email provider's domain with www", `{"name":"Omicron Example","domains":["www.gmail.com"]}`, false},
		{"an email provider's domain in capitals", `{"name":"Nu Example","domains":["GMAIL.com"]}`, false},
		{"an unknown field", `{"name":"Xi Example","industry":"software"}`, false},
	},
	"user": {
		{"a human", `{"key":"zoe-acme","kind":"human","holder":"org_acme-example"}`, true},
		{"a named human", `{"key":"max-acme","kind":"human","holder":"org_acme-example","name":"Max Example"}`, true},
		{"an agent with its handler", `{"key":"bot3-acme","kind":"agent","holder":"org_acme-example","handler":"us_ana-acme"}`, true},
		{"no key", `{"kind":"human","holder":"org_acme-example"}`, false},
		{"no kind", `{"key":"nokind-acme","holder":"org_acme-example"}`, false},
		{"no holder", `{"key":"noholder-acme","kind":"human"}`, false},
		{"a key with capitals and spaces", `{"key":"Zoe Acme","kind":"human","holder":"org_acme-example"}`, false},
		{"an agent without a handler", `{"key":"nohandler-acme","kind":"agent","holder":"org_acme-example"}`, false},
		{"a human with a handler", `{"key":"handled-acme","kind":"human","holder":"org_acme-example","handler":"us_ana-acme"}`, false},
		{"an agent linked to a person", `{"key":"linked-acme","kind":"agent","holder":"org_acme-example","handler":"us_ana-acme","person":"pe_elena-ruiz"}`, false},
		{"a kind that does not exist", `{"key":"robot-acme","kind":"robot","holder":"org_acme-example"}`, false},
		{"a holder that is not an organization ID", `{"key":"badholder-acme","kind":"human","holder":"acme"}`, false},
		{"an unknown field", `{"key":"role-acme","kind":"human","holder":"org_acme-example","role":"admin"}`, false},
	},
}

// flagOf is the CLI flag of an input field, where it is not the field's
// name with hyphens.
var flagOf = map[string]string{
	"participants": "participant", "refs": "ref", "emails": "email", "domains": "domain",
	"summary": "summary-file", "description": "description-file",
}

// cliArgs turns an input document into the arguments of its create command
// and the standard input that long texts come in.
func cliArgs(t *testing.T, typ, doc string) (args []string, stdin string) {
	t.Helper()
	var fields map[string]any
	must(t, json.Unmarshal([]byte(doc), &fields))
	args = []string{typ, "create"}
	var keys []string
	for k := range fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := fields[k]
		flag := "--" + strings.ReplaceAll(k, "_", "-")
		if f, ok := flagOf[k]; ok {
			flag = "--" + f
		}
		switch {
		case typ == "user" && k == "key":
			args = append(args, fmt.Sprint(v))
		case typ == "user" && k == "kind":
			args = append(args, "--"+fmt.Sprint(v))
		case v == true:
			args = append(args, flag)
		case v == false:
		default:
			if list, ok := v.([]any); ok {
				for _, x := range list {
					args = append(args, flag, fmt.Sprint(x))
				}
			} else if k == "summary" || k == "description" {
				args, stdin = append(args, flag, "-"), fmt.Sprint(v)
			} else {
				args = append(args, flag, fmt.Sprint(v))
			}
		}
	}
	return args, stdin
}

func TestSchemaMatchesValidation(t *testing.T) {
	for _, typ := range schemaTypes {
		t.Run(typ, func(t *testing.T) {
			e := taskWorld(t)
			s := schemaOf(t, e, typ)
			// The documented flags are the CLI's.
			for name, p := range obj(t, s, "properties") {
				cli := p.(map[string]any)["x-cli"].(string)
				if typ == "user" && (name == "key" || name == "kind") {
					continue
				}
				flag := "--" + strings.ReplaceAll(name, "_", "-")
				if f, ok := flagOf[name]; ok {
					flag = "--" + f
				}
				if !strings.HasPrefix(cli, flag) {
					t.Errorf("%s: x-cli %q, want %s", name, cli, flag)
				}
			}
			for _, c := range corpus[typ] {
				t.Run(c.name, func(t *testing.T) {
					var doc any
					must(t, json.Unmarshal([]byte(c.doc), &doc))
					if problem := validate(t, s, doc); (problem == "") != c.ok {
						t.Fatalf("schema: accepted=%v, want %v (%s)", problem == "", c.ok, problem)
					}
					args, stdin := cliArgs(t, typ, c.doc)
					r := e.runWithStdin(stdin, args...)
					if c.ok {
						if r.Code != 0 {
							t.Fatalf("the schema accepts it, Kravis refuses it: %v: %s", args, r.Stderr)
						}
						return
					}
					code := ""
					if r.Code != 0 {
						code, _ = errorOf(t, r)["code"].(string)
					}
					if !slices.Contains([]string{"INVALID_VALUE", "UNKNOWN_FIELD", "NOT_FOUND"}, code) {
						t.Fatalf("the schema rejects it, Kravis answers %d %q (%v)", r.Code, code, r.Stderr)
					}
				})
			}
		})
	}
}

// validate checks doc against the subset of JSON Schema that kravis schema
// uses, and returns the first problem. Writing it here keeps Kravis free of
// a validator dependency.
func validate(t *testing.T, schema map[string]any, doc any) string {
	t.Helper()
	str := func(k string) string { s, _ := schema[k].(string); return s }
	if ty := str("type"); ty != "" {
		ok := false
		switch ty {
		case "object":
			_, ok = doc.(map[string]any)
		case "array":
			_, ok = doc.([]any)
		case "string":
			_, ok = doc.(string)
		case "boolean":
			_, ok = doc.(bool)
		default:
			t.Fatalf("unsupported type %q", ty)
		}
		if !ok {
			return fmt.Sprintf("%v is not a %s", doc, ty)
		}
	}
	if enum, ok := schema["enum"].([]any); ok && !slices.Contains(enum, doc) {
		return fmt.Sprintf("%v is not one of %v", doc, enum)
	}
	if s, ok := doc.(string); ok {
		if p := str("pattern"); p != "" && !regexp.MustCompile(p).MatchString(s) {
			return fmt.Sprintf("%q does not match %s", s, p)
		}
		switch str("format") {
		case "date":
			if d, err := time.Parse("2006-01-02", s); err != nil || d.Format("2006-01-02") != s {
				return fmt.Sprintf("%q is not a date", s)
			}
		case "date-time":
			if _, err := time.Parse(time.RFC3339, s); err != nil {
				return fmt.Sprintf("%q is not a date-time", s)
			}
		}
	}
	if c, ok := schema["const"]; ok && doc != c {
		return fmt.Sprintf("%v is not %v", doc, c)
	}
	if all, ok := schema["allOf"].([]any); ok {
		for _, a := range all {
			if p := validate(t, a.(map[string]any), doc); p != "" {
				return p
			}
		}
	}
	if cond, ok := schema["if"].(map[string]any); ok && validate(t, cond, doc) == "" {
		if then, ok := schema["then"].(map[string]any); ok {
			if p := validate(t, then, doc); p != "" {
				return p
			}
		}
	}
	if not, ok := schema["not"].(map[string]any); ok && validate(t, not, doc) == "" {
		return fmt.Sprintf("%v matches a schema it must not", doc)
	}
	if anyOf, ok := schema["anyOf"].([]any); ok {
		found := false
		for _, a := range anyOf {
			found = found || validate(t, a.(map[string]any), doc) == ""
		}
		if !found {
			return fmt.Sprintf("%v matches none of anyOf", doc)
		}
	}
	if list, ok := doc.([]any); ok {
		if n, ok := schema["minItems"].(float64); ok && float64(len(list)) < n {
			return "too few items"
		}
		if item, ok := schema["items"].(map[string]any); ok {
			for _, x := range list {
				if p := validate(t, item, x); p != "" {
					return p
				}
			}
		}
	}
	if m, ok := doc.(map[string]any); ok {
		props, _ := schema["properties"].(map[string]any)
		for _, r := range anyStrings(schema["required"]) {
			if _, ok := m[r]; !ok {
				return "missing " + r
			}
		}
		for k, v := range m {
			p, known := props[k].(map[string]any)
			switch {
			case !known && schema["additionalProperties"] == false:
				return "unknown field " + k
			case known:
				if problem := validate(t, p, v); problem != "" {
					return k + ": " + problem
				}
			}
		}
	}
	return ""
}

func anyStrings(v any) []string {
	var out []string
	l, _ := v.([]any)
	for _, x := range l {
		out = append(out, x.(string))
	}
	return out
}
