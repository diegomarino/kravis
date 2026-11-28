package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file holds the checks of the contract test: the cases are in
// contract_cases_test.go and the states they run in in contract_world_test.go.

// docCode is a row of the error and warning codes table of docs/cli.md.
type docCode struct {
	Error   bool // an error code
	Exit    int  // its exit code, for an error
	Warning bool
	Finding bool // a check finding
}

// contractDoc reads docs/cli.md.
func contractDoc(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoDir, "docs", "cli.md"))
	must(t, err)
	return string(raw)
}

// docCodes parses the table of error and warning codes.
func docCodes(t *testing.T, doc string) map[string]docCode {
	t.Helper()
	_, after, ok := strings.Cut(doc, "### Error and warning codes")
	if !ok {
		t.Fatal("docs/cli.md has no table of error and warning codes")
	}
	table, _, _ := strings.Cut(after, "A check finding is data")
	codes := map[string]docCode{}
	for _, line := range strings.Split(table, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			continue
		}
		code := strings.Trim(strings.TrimSpace(cells[1]), "`")
		kind := strings.TrimSpace(cells[2])
		d := docCode{Warning: kind == "warning", Finding: strings.Contains(kind, "check finding")}
		if rest, ok := strings.CutPrefix(kind, "error, "); ok {
			n, _, _ := strings.Cut(rest, ";")
			exit, err := strconv.Atoi(strings.TrimSpace(n))
			must(t, err)
			d.Error, d.Exit = true, exit
		}
		codes[code] = d
	}
	if len(codes) < 20 {
		t.Fatalf("only %d codes parsed from docs/cli.md: the parser is broken", len(codes))
	}
	return codes
}

// docOutcomes are the outcomes the contract names.
var docOutcomes = []string{"created", "existing", "updated", "unchanged", "deleted"}

// produced is what the cases brought out, by kind.
type produced struct{ errors, warnings, findings map[string]bool }

func newProduced() *produced {
	return &produced{errors: map[string]bool{}, warnings: map[string]bool{}, findings: map[string]bool{}}
}

// subst replaces {name} in s by the variables of the world.
func subst(s string, v vars) string {
	for k, val := range v {
		s = strings.ReplaceAll(s, "{"+k+"}", val)
	}
	return s
}

func substAll(args []string, v vars) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = subst(a, v)
	}
	return out
}

// leadingGlobals skips the global flags a command line may start with.
func leadingGlobals(args []string) []string {
	for len(args) > 0 {
		switch {
		case (args[0] == "--actor" || args[0] == "--output") && len(args) > 1:
			args = args[2:]
		case args[0] == "--verbose":
			args = args[1:]
		default:
			return args
		}
	}
	return args
}

// commandOf names the command a command line runs: the longest run of words
// at its start that is a command of the tree, "" for the root, and false for
// words that are no command.
func commandOf(tree map[string][]string, args []string) (string, bool) {
	prefixes := map[string]bool{"": true}
	for c := range tree {
		ws := strings.Fields(c)
		for i := 1; i <= len(ws); i++ {
			prefixes[strings.Join(ws[:i], " ")] = true
		}
	}
	args = leadingGlobals(args)
	path := ""
	for _, a := range args {
		next := strings.TrimSpace(path + " " + a)
		if strings.HasPrefix(a, "-") || !prefixes[next] {
			break
		}
		path = next
	}
	_, ok := tree[path]
	return path, ok
}

// flagsOf returns the flags of a command line, without values.
func flagsOf(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "--") && len(a) > 2 {
			f, _, _ := strings.Cut(a, "=")
			out = append(out, f)
		}
	}
	return out
}

// universalFlags are the persistent flags of the root: a case that uses one
// anywhere covers it; every other flag is covered per command.
var universalFlags = []string{"--actor", "--output", "--verbose"}

// TestContractCasesCoverTheCommandTree fails naming every command, and every
// flag of a command, that no case exercises.
func TestContractCasesCoverTheCommandTree(t *testing.T) {
	tree := treeOf(t)
	cases := contractCases()
	cmds := map[string]bool{}
	pairs := map[string]bool{}
	names := map[string]bool{}
	for _, c := range cases {
		path, ok := commandOf(tree, c.Args)
		if !ok {
			if c.Code != "INVALID_VALUE" && c.Code != "UNKNOWN_FIELD" {
				t.Errorf("case %v runs no command of the tree", c.Args)
			}
			continue
		}
		cmds[path] = true
		for _, f := range flagsOf(c.Args) {
			pairs[path+" "+f] = true
			names[f] = true
		}
	}
	var missingCmds, missingFlags []string
	total, covered := 0, 0
	for path, flags := range tree {
		total++
		if cmds[path] {
			covered++
		} else {
			missingCmds = append(missingCmds, "kravis "+path)
		}
		for _, f := range flags {
			if slices.Contains(universalFlags, f) {
				continue
			}
			if f == "--version" && path == "" {
				continue
			}
			total++
			if pairs[path+" "+f] {
				covered++
			} else {
				missingFlags = append(missingFlags, "kravis "+path+" "+f)
			}
		}
	}
	for _, f := range universalFlags {
		total++
		if names[f] {
			covered++
		} else {
			missingFlags = append(missingFlags, f+" (anywhere)")
		}
	}
	total++ // --version of the root
	if names["--version"] {
		covered++
	} else {
		missingFlags = append(missingFlags, "kravis --version")
	}
	sort.Strings(missingCmds)
	sort.Strings(missingFlags)
	if len(missingCmds) > 0 {
		t.Errorf("no case runs these commands:\n  %s", strings.Join(missingCmds, "\n  "))
	}
	if len(missingFlags) > 0 {
		t.Errorf("no case uses these flags:\n  %s", strings.Join(missingFlags, "\n  "))
	}
	t.Logf("commands and flags covered: %d of %d (%d commands, %d cases)", covered, total, len(tree), len(cases))
}

// TestContractHelpAndDocAgreeWithTheTree checks that --help of every command
// lists the flags of the tree, that every flag is in docs/cli.md and that every
// flag docs/cli.md names exists.
func TestContractHelpAndDocAgreeWithTheTree(t *testing.T) {
	tree := treeOf(t)
	doc := contractDoc(t)
	e := newEnv(t)

	// --help of every command lists exactly the flags of the tree, plus --help.
	helpFlag := regexp.MustCompile(`(?m)^\s+(?:-\w, )?(--[a-z][a-z-]*)`)
	all := map[string]bool{}
	for path, flags := range tree {
		args := append(strings.Fields(path), "--help")
		r := e.run(args...)
		if r.Code != 0 || r.Stderr != "" || strings.HasPrefix(r.Stdout, "{") {
			t.Errorf("kravis %s: --help gave code %d, stdout %q, stderr %q", strings.Join(args, " "), r.Code, r.Stdout, r.Stderr)
			continue
		}
		var got []string
		for _, m := range helpFlag.FindAllStringSubmatch(r.Stdout, -1) {
			got = append(got, m[1])
		}
		want := append(slices.Clone(flags), "--help")
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("kravis %s --help lists %v, want %v", path, got, want)
		}
		for _, f := range flags {
			all[f] = true
		}
	}

	// Every flag of the CLI is in docs/cli.md.
	docFlags := flagsInDoc(doc)
	for f := range all {
		// `--remove-<flag>` is the documented counterpart of a repeatable flag.
		if base, ok := strings.CutPrefix(f, "--remove-"); ok && docFlags["--"+base] && strings.Contains(doc, "--remove-<flag>") {
			continue
		}
		if !docFlags[f] {
			t.Errorf("docs/cli.md never mentions %s", f)
		}
	}
	// Every flag of docs/cli.md exists, except the ones it says do not exist.
	absent := map[string]bool{"--pipeline": true, "--match": true, "--request-id": true, "--group-by": true,
		"--fix": true, "--owner": true, "--delegate": true}
	for f := range docFlags {
		switch {
		case absent[f]:
			if all[f] {
				t.Errorf("docs/cli.md says %s does not exist, but the CLI has it", f)
			}
		case f == "--help":
		case !all[f]:
			t.Errorf("docs/cli.md names %s, which no command has", f)
		}
	}

	// A table row names commands and the flags they take: each flag of the row
	// belongs to one of its commands (the prose describes the shared ones).
	for _, row := range commandRows(doc) {
		for _, c := range row.commands {
			if _, ok := tree[c]; !ok {
				t.Errorf("docs/cli.md row names `kravis %s`, which is no command", c)
			}
		}
		for f := range row.flags {
			if absent[f] || slices.Contains(universalFlags, f) || f == "--help" {
				continue
			}
			found := false
			for _, c := range row.commands {
				found = found || slices.Contains(tree[c], f)
			}
			if !found {
				t.Errorf("docs/cli.md row of %v names %s, which none of them has", row.commands, f)
			}
		}
	}
}

var docFlag = regexp.MustCompile(`--[a-z][a-z-]*[a-z]`)

// flagsInDoc returns every flag docs/cli.md writes, except the template
// `--remove-<flag>`.
func flagsInDoc(text string) map[string]bool {
	out := map[string]bool{}
	text = strings.ReplaceAll(text, "--remove-<flag>", "") // a template, not a flag
	for _, m := range docFlag.FindAllString(text, -1) {
		out[m] = true
	}
	return out
}

type docRow struct {
	commands []string
	flags    map[string]bool
}

// commandRows reads the rows of the command tables: "| `kravis <command…>
// …` | notes |". A command such as `user list|get|create` stands for each of
// its verbs; the flags are every flag the row writes, in either cell.
func commandRows(doc string) []docRow {
	var rows []docRow
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(line, "| `kravis") {
			continue
		}
		line = strings.ReplaceAll(line, `\|`, "|")
		cells := strings.SplitN(strings.TrimPrefix(line, "|"), "` |", 2)
		spec := strings.TrimPrefix(strings.TrimSpace(cells[0]), "`")
		var words []string
		for _, tok := range strings.Fields(spec)[1:] {
			if strings.ContainsAny(tok[:1], "-<[") {
				break
			}
			words = append(words, tok)
		}
		var commands []string
		if n := len(words); n > 0 && strings.Contains(words[n-1], "|") {
			for _, verb := range strings.Split(words[n-1], "|") {
				commands = append(commands, strings.Join(append(slices.Clone(words[:n-1]), verb), " "))
			}
		} else {
			commands = []string{strings.Join(words, " ")}
		}
		rows = append(rows, docRow{commands: commands, flags: flagsInDoc(line)})
	}
	return rows
}

// TestCLIMatchesTheContract runs every case and checks its output against
// docs/cli.md: the shape of the JSON, the exit code of its error code, the
// outcome and the warnings; then that every code of the contract came out of
// some case and no other did.
func TestCLIMatchesTheContract(t *testing.T) {
	started := time.Now()
	doc := contractDoc(t)
	codes := docCodes(t, doc)
	tree := treeOf(t)
	w := newContractWorlds(t)
	got := newProduced()
	cases := contractCases()
	for i, c := range cases {
		name := fmt.Sprintf("%03d %s: %s", i, c.State, strings.Join(c.Args, " "))
		t.Run(name, func(t *testing.T) { runContractCase(t, w, tree, codes, got, c) })
	}
	t.Run("every code of the contract is produced, and no other", func(t *testing.T) {
		check := func(kind string, want func(docCode) bool, got map[string]bool) {
			n := 0
			for code, d := range codes {
				if want(d) {
					n++
					if !got[code] {
						t.Errorf("no case produces the %s %s", kind, code)
					}
				}
			}
			for code := range got {
				if !want(codes[code]) {
					t.Errorf("a case produced the %s %s, which docs/cli.md does not list as one", kind, code)
				}
			}
			t.Logf("%s codes produced: %d of %d", kind, len(got), n)
		}
		check("error", func(d docCode) bool { return d.Error }, got.errors)
		check("warning", func(d docCode) bool { return d.Warning }, got.warnings)
		check("check finding", func(d docCode) bool { return d.Finding }, got.findings)
	})
	t.Logf("%d cases in %s", len(cases), time.Since(started).Round(time.Millisecond))
}

func runContractCase(t *testing.T, w *worlds, tree map[string][]string, codes map[string]docCode, got *produced, c contractCase) {
	e, v := w.instance(t, c.State)
	for k, val := range c.Env {
		t.Setenv(k, subst(val, v))
	}
	e.TTY = c.TTY
	for _, step := range c.Before {
		r := e.run(substAll(step, v)...)
		if r.Code != 0 {
			t.Fatalf("before %v: code %d, stderr %s", step, r.Code, r.Stderr)
		}
		if cur, ok := r.JSON(t)["next_cursor"].(string); ok {
			v["cursor"] = cur
		}
	}
	args := substAll(c.Args, v)
	before := e.Files()
	r := e.runWithStdin(c.Stdin, args...)
	if c.Code != "" {
		checkContractError(t, e, tree, codes, got, c, args, r)
		if !maps.Equal(before, e.Files()) {
			t.Errorf("a refusal changed a file")
		}
		return
	}
	checkContractSuccess(t, tree, codes, got, c, args, r)
}

// oneLine fails unless s is one JSON object on one line; it returns it.
func oneLine(t *testing.T, what, s string) map[string]any {
	t.Helper()
	if !strings.HasSuffix(s, "\n") || strings.Count(s, "\n") != 1 {
		t.Fatalf("%s must be one line, got %q", what, s)
	}
	return decodeObject(t, s)
}

func checkContractError(t *testing.T, e *env, tree map[string][]string, codes map[string]docCode, got *produced, c contractCase, args []string, r result) {
	t.Helper()
	d, ok := codes[c.Code]
	if !ok || !d.Error {
		t.Fatalf("the case expects %s, which docs/cli.md does not list as an error", c.Code)
	}
	if r.Stdout != "" {
		t.Errorf("an error leaves stdout empty, got %q", r.Stdout)
	}
	out := oneLine(t, "stderr", r.Stderr)
	if len(out) != 1 || out["error"] == nil {
		t.Fatalf("stderr must hold only an error, got %v", out)
	}
	eo, _ := out["error"].(map[string]any)
	for k := range eo {
		if !slices.Contains([]string{"code", "message", "details", "retryable", "hint"}, k) {
			t.Errorf("error has the unexpected field %q", k)
		}
	}
	code, _ := eo["code"].(string)
	got.errors[code] = true
	if code != c.Code {
		t.Fatalf("error code %q, want %s (%s)", code, c.Code, r.Stderr)
	}
	if dd, ok := codes[code]; !ok || !dd.Error {
		t.Errorf("error code %s is not an error code of docs/cli.md", code)
	} else if r.Code != dd.Exit {
		t.Errorf("exit code %d, docs/cli.md gives %d to %s", r.Code, dd.Exit, code)
	}
	if m, _ := eo["message"].(string); m == "" {
		t.Error("error has no message")
	}
	details, isObj := eo["details"].(map[string]any)
	if !isObj {
		t.Errorf("error details must be an object, got %v", eo["details"])
	}
	// The contract: retryable only for VERSION_CONFLICT and STORAGE_UNAVAILABLE,
	// and not for the latter when no workspace is found.
	wantRetry := (code == "VERSION_CONFLICT" || code == "STORAGE_UNAVAILABLE") && c.State != baseNone
	if retry, ok := eo["retryable"].(bool); !ok || retry != wantRetry {
		t.Errorf("retryable = %v, want %v", eo["retryable"], wantRetry)
	}
	hint, _ := eo["hint"].(string)
	if _, has := eo["hint"]; has && hint == "" {
		t.Error("hint is present but empty")
	}
	_ = details // docs/cli.md makes the hint optional: present when details do not show the next step
	if _, has := out["warnings"]; has {
		t.Error("an error never carries warnings")
	}
}

func checkContractSuccess(t *testing.T, tree map[string][]string, codes map[string]docCode, got *produced, c contractCase, args []string, r result) {
	t.Helper()
	wantExit := 0
	if len(c.Findings) > 0 {
		wantExit = 6
	}
	if r.Code != wantExit {
		t.Fatalf("exit code %d, want %d (stdout %s, stderr %s)", r.Code, wantExit, r.Stdout, r.Stderr)
	}
	if r.Stderr != "" && !slices.Contains(args, "--verbose") {
		t.Errorf("a success leaves stderr empty, got %q", r.Stderr)
	}
	out := oneLine(t, "stdout", r.Stdout)
	for k := range out {
		if !slices.Contains([]string{"data", "outcome", "warnings", "filters", "total", "next_cursor"}, k) {
			t.Errorf("the response has the unexpected field %q", k)
		}
	}
	if out["data"] == nil {
		t.Fatalf("data is missing or null: %s", r.Stdout)
	}

	words, _ := commandOf(tree, args)
	verb := ""
	if ws := strings.Fields(words); len(ws) > 0 {
		verb = ws[len(ws)-1]
	}
	isList := verb == "list" || words == "search"
	isRead := isList || verb == "get"

	// outcome
	if c.Outcome == "" {
		if _, has := out["outcome"]; has {
			t.Errorf("a read has no outcome, got %v", out["outcome"])
		}
	} else if out["outcome"] != c.Outcome {
		t.Errorf("outcome %v, want %s", out["outcome"], c.Outcome)
	}
	if o, has := out["outcome"]; has && !slices.Contains(docOutcomes, fmt.Sprint(o)) {
		t.Errorf("outcome %v is not one docs/cli.md names", o)
	}
	// filters only on reads
	if f, has := out["filters"]; has {
		if !isRead {
			t.Errorf("filters appear only on reads, but %q has them", words)
		}
		if _, ok := f.(map[string]any); !ok {
			t.Errorf("filters must be an object, got %v", f)
		}
	}
	// lists have total and next_cursor, nothing else does
	_, hasTotal := out["total"]
	_, hasCursor := out["next_cursor"]
	if isList {
		if _, ok := out["data"].([]any); !ok {
			t.Errorf("a list's data must be an array, got %v", out["data"])
		}
		if n, ok := out["total"].(float64); !ok || n < 0 {
			t.Errorf("a list has a total, got %v", out["total"])
		}
		if !hasCursor {
			t.Error("a list always has next_cursor, null on the last page")
		} else if cur := out["next_cursor"]; cur != nil {
			if s, ok := cur.(string); !ok || s == "" {
				t.Errorf("next_cursor is a string or null, got %v", cur)
			}
		}
		if lim := flagValue(args, "--limit"); lim != "" {
			if n, err := strconv.Atoi(lim); err == nil && n > 0 && len(out["data"].([]any)) > n {
				t.Errorf("--limit %d gave %d records", n, len(out["data"].([]any)))
			}
		}
	} else if hasTotal || hasCursor {
		t.Errorf("total and next_cursor belong to lists, but %q has them", words)
	}
	// warnings only when present, each a code and a message
	var warned []string
	if ws, has := out["warnings"]; has {
		list, _ := ws.([]any)
		if len(list) == 0 {
			t.Error("warnings appears only when there are warnings")
		}
		for _, x := range list {
			wm, _ := x.(map[string]any)
			code, _ := wm["code"].(string)
			if m, _ := wm["message"].(string); m == "" || code == "" {
				t.Errorf("a warning has a code and a message, got %v", wm)
			}
			if d, ok := codes[code]; !ok || !d.Warning {
				t.Errorf("warning %s is not a warning code of docs/cli.md", code)
			}
			got.warnings[code] = true
			warned = append(warned, code)
		}
	}
	if !slices.Equal(warned, c.Warnings) {
		t.Errorf("warnings %v, want %v", warned, c.Warnings)
	}
	// check: findings are data, each with a code, a record and a message
	if words == "check" {
		list, ok := out["data"].([]any)
		if !ok {
			t.Fatalf("check data must be an array, got %v", out["data"])
		}
		var found []string
		for _, x := range list {
			fm, _ := x.(map[string]any)
			code, _ := fm["code"].(string)
			if fm["record"] == nil || fm["message"] == nil {
				t.Errorf("a finding has a code, a record and a message, got %v", fm)
			}
			if d, ok := codes[code]; !ok || !d.Finding {
				t.Errorf("finding %s is not a check finding of docs/cli.md", code)
			}
			got.findings[code] = true
			if !slices.Contains(found, code) {
				found = append(found, code)
			}
		}
		slices.Sort(found)
		want := slices.Clone(c.Findings)
		slices.Sort(want)
		if !slices.Equal(found, want) {
			t.Errorf("findings %v, want %v (%s)", found, want, r.Stdout)
		}
	}
}

// flagValue returns the value after flag in args, "" when there is none.
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

var _ = json.Marshal

// commandTree is the tree of runnable commands with their flags, as the hidden
// tree command prints it, built without running anything.
func commandTree() map[string][]string {
	tree := map[string][]string{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		if c.Runnable() {
			path := strings.TrimSpace(strings.TrimPrefix(c.CommandPath(), c.Root().Name()))
			flags := []string{}
			add := func(f *pflag.Flag) {
				if !f.Hidden && !slices.Contains(flags, "--"+f.Name) {
					flags = append(flags, "--"+f.Name)
				}
			}
			c.LocalFlags().VisitAll(add)
			c.InheritedFlags().VisitAll(add)
			tree[path] = flags
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newApp(defaultDeps()).root())
	return tree
}
