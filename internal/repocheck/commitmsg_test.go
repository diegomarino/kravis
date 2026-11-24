package repocheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommitMsgHook runs .githooks/commit-msg on sample subjects. CI refuses a
// whole subject longer than 72 characters; the hook has to agree.
func TestCommitMsgHook(t *testing.T) {
	top := root(t)
	hook := filepath.Join(top, ".githooks", "commit-msg")
	// pad returns "feat: " plus enough letters to reach n characters.
	pad := func(n int) string { return "feat: " + strings.Repeat("a", n-len("feat: ")) }
	cases := []struct {
		name    string
		subject string
		ok      bool
	}{
		{"71 characters", pad(71), true},
		{"72 characters", pad(72), true},
		{"73 characters", pad(73), false},
		{"valid type with scope", "fix(cli): no workspace is not retryable", true},
		{"invalid type", "wip: something", false},
		{"merge is exempt", "Merge branch " + strings.Repeat("a", 80), true},
		{"fixup is exempt", "fixup! " + strings.Repeat("a", 80), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := filepath.Join(t.TempDir(), "MSG")
			if err := os.WriteFile(msg, []byte(c.subject+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("sh", hook, msg).CombinedOutput()
			if c.ok && err != nil {
				t.Errorf("%q refused: %s", c.subject, out)
			}
			if !c.ok && err == nil {
				t.Errorf("%q accepted, want refused", c.subject)
			}
		})
	}
}
