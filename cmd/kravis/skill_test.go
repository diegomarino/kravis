package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The skill and the checkout, as absolute paths: tests move into a workspace.
var (
	skillDir, _ = filepath.Abs("../../skills/kravis")
	repoDir, _  = filepath.Abs("../..")
)

// skillFiles are the Markdown files of the skill.
func skillFiles(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(skillDir + "/*.md")
	must(t, err)
	more, err := filepath.Glob(skillDir + "/references/*.md")
	must(t, err)
	out := map[string]string{}
	for _, p := range append(paths, more...) {
		raw, err := os.ReadFile(p)
		must(t, err)
		rel, err := filepath.Rel(skillDir, p)
		must(t, err)
		out[filepath.ToSlash(rel)] = string(raw)
	}
	if out["SKILL.md"] == "" || out["references/message-id.md"] == "" {
		t.Fatalf("the skill lacks SKILL.md or references/message-id.md: %v", slices.Collect(func(yield func(string) bool) {
			for k := range out {
				if !yield(k) {
					return
				}
			}
		}))
	}
	return out
}

// treeOf runs the hidden tree command: every command with its flags.
func treeOf(t *testing.T) map[string][]string {
	t.Helper()
	e := newEnv(t)
	r := e.run("tree")
	if r.Code != 0 {
		t.Fatalf("tree: %d %s", r.Code, r.Stderr)
	}
	var out struct {
		Data []commandNode `json:"data"`
	}
	must(t, json.Unmarshal([]byte(r.Stdout), &out))
	tree := map[string][]string{}
	for _, n := range out.Data {
		tree[n.Command] = n.Flags
	}
	return tree
}

// citedCommands returns every command a Markdown text cites: each code
// block line or inline code that starts with kravis, with continued lines
// joined and cut at a pipe, && or ;.
func citedCommands(text string) []string {
	var cited []string
	inFence := false
	var pending string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		var candidates []string
		if inFence {
			if strings.HasSuffix(line, `\`) {
				pending += strings.TrimSuffix(line, `\`) + " "
				continue
			}
			candidates = []string{pending + line}
			pending = ""
		} else {
			for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(line, -1) {
				candidates = append(candidates, m[1])
			}
		}
		for _, c := range candidates {
			c = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c), "$ "))
			if c == "kravis" || strings.HasPrefix(c, "kravis ") {
				cited = append(cited, c)
			}
		}
	}
	return cited
}

// skillProblems compares what a text cites with the command tree: the words
// up to the first argument that starts with - or < or [ or is quoted name a
// command, and every flag after them belongs to it.
func skillProblems(text string, tree map[string][]string) []string {
	var problems []string
	for _, c := range citedCommands(text) {
		for _, sep := range []string{"|", "&&", ";", "#"} {
			c, _, _ = strings.Cut(c, sep)
		}
		var words, flags []string
		inWords := true
		for _, tok := range strings.Fields(c)[1:] {
			tok = strings.TrimLeft(tok, "[")
			switch {
			case strings.HasPrefix(tok, "--"):
				inWords = false
				flag, _, _ := strings.Cut(strings.TrimRight(tok, "]"), "=")
				flags = append(flags, flag)
			case inWords && tok != "" && !strings.ContainsAny(tok[:1], "-<\"'"):
				words = append(words, tok)
			default:
				inWords = false
			}
		}
		path := strings.Join(words, " ")
		have, ok := tree[path]
		if !ok {
			problems = append(problems, "no command `kravis "+path+"` in: "+c)
			continue
		}
		for _, f := range flags {
			if f != "--help" && !slices.Contains(have, f) { // --help belongs to every command
				problems = append(problems, "`kravis "+path+"` has no flag "+f+" in: "+c)
			}
		}
	}
	return problems
}

func TestSkillDoesNotDrift(t *testing.T) {
	tree := treeOf(t)
	cited := 0
	for name, text := range skillFiles(t) {
		cited += len(citedCommands(text))
		if problems := skillProblems(text, tree); len(problems) > 0 {
			t.Errorf("%s cites what the binary lacks:\n%s", name, strings.Join(problems, "\n"))
		}
	}
	if cited < 10 {
		t.Errorf("only %d commands cited: the extraction is broken", cited)
	}
}

func TestSkillDriftIsDetected(t *testing.T) {
	tree := treeOf(t)
	for _, tc := range []struct{ text, want string }{
		{"Run `kravis task frobnicate <task>`.", "no command `kravis task frobnicate`"},
		{"Run `kravis task create --title <t> --assignee <u> --owner <u>`.", "has no flag --owner"},
		{"```sh\nkravis interaction create \\\n  --channel email --invented x\n```", "has no flag --invented"},
		{"Run `kravis task create --help`.", ""},
		{"Run `kravis whoami --set <user>` and `kravis task list --overdue | jq .`.", ""},
	} {
		problems := strings.Join(skillProblems(tc.text, tree), "\n")
		if tc.want == "" && problems != "" || !strings.Contains(problems, tc.want) {
			t.Errorf("%q: problems %q, want %q", tc.text, problems, tc.want)
		}
	}
}

func TestSkillHasValidFrontmatter(t *testing.T) {
	text := skillFiles(t)["SKILL.md"]
	front, _, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !ok || !strings.HasPrefix(text, "---\n") {
		t.Fatal("SKILL.md has no frontmatter")
	}
	fields := map[string]string{}
	for _, l := range strings.Split(front, "\n") {
		k, v, _ := strings.Cut(l, ": ")
		fields[k] = v
	}
	if fields["name"] != "kravis" || len(fields["description"]) < 40 {
		t.Errorf("frontmatter %v", fields)
	}
}

// TestSkillReferencesAreAccepted records every reference example of
// references/message-id.md through the CLI.
func TestSkillReferencesAreAccepted(t *testing.T) {
	var refs []string
	for _, m := range regexp.MustCompile(`--ref (\S+)`).FindAllStringSubmatch(skillFiles(t)["references/message-id.md"], -1) {
		if !strings.Contains(m[1], "<") && !slices.Contains(refs, m[1]) {
			refs = append(refs, m[1])
		}
	}
	if len(refs) < 4 {
		t.Fatalf("only %d reference examples found: %v", len(refs), refs)
	}
	e := taskWorld(t)
	for i, ref := range refs {
		r := e.run(record("Example "+string(rune('a'+i)), "--ref", ref)...)
		if r.Code != 0 {
			t.Errorf("%s: %d %s", ref, r.Code, r.Stderr)
			continue
		}
		if slices.Contains(warningCodes(r.JSON(t)), "UNKNOWN_SOURCE") {
			t.Errorf("%s is not in the catalog of sources", ref)
		}
	}
}

// skillsVersion is the reviewed, exact version of the skills installer;
// raising it is a deliberate change.
const skillsVersion = "1.9.0"

// TestSkillInstalls installs the skill from the checkout with npx skills
// into a temporary folder. It needs the network, so it runs only where
// SKILL_INSTALL_TEST is set, as in CI.
func TestSkillInstalls(t *testing.T) {
	if os.Getenv("SKILL_INSTALL_TEST") == "" {
		t.Skip("set SKILL_INSTALL_TEST=1 to install the skill with npx skills (needs the network)")
	}
	e := newEnv(t)
	dir := t.TempDir()
	cmd := exec.Command("npx", "--yes", "skills@"+skillsVersion, "add", repoDir, "--skill", "kravis", "--yes", "--agent", "claude-code")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+e.Home, "USERPROFILE="+e.Home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("npx skills add: %v\n%s", err, out)
	}
	for _, f := range []string{"SKILL.md", "references/message-id.md"} {
		if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "kravis", filepath.FromSlash(f))); err != nil {
			t.Errorf("the installed skill lacks %s: %v", f, err)
		}
	}
}

// The installed skill has no repository: it never links to repository files.
func TestSkillLinksOnlyToItsOwnFiles(t *testing.T) {
	link := regexp.MustCompile(`\]\(([^)]+)\)`)
	for name, text := range skillFiles(t) {
		for _, m := range link.FindAllStringSubmatch(text, -1) {
			target := m[1]
			if strings.HasPrefix(target, "http") {
				continue
			}
			if strings.Contains(target, "..") || strings.Contains(target, "docs/") {
				t.Errorf("%s links to %s, outside the skill", name, target)
			}
		}
	}
}
