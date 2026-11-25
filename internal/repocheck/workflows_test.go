package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var pinnedUse = regexp.MustCompile(`^\s*-?\s*uses: [\w./-]+@[0-9a-f]{40} # v\d+(\.\d+)*$`)

// TestWorkflowActionsArePinned requires every `uses:` to name a full commit
// SHA with a version comment, and pull-request writes only in the
// release-please job.
func TestWorkflowActionsArePinned(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(root(t), ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("workflows: %v %v", files, err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		job := ""
		for i, line := range strings.Split(string(data), "\n") {
			if m := regexp.MustCompile(`^  ([\w-]+):$`).FindStringSubmatch(line); m != nil {
				job = m[1]
			}
			if strings.Contains(line, "uses:") && !pinnedUse.MatchString(line) {
				t.Errorf("%s:%d is not pinned to a full SHA with a version comment: %s", filepath.Base(f), i+1, line)
			}
			if strings.Contains(line, "pull-requests: write") && job != "release-please" {
				t.Errorf("%s:%d grants pull-requests: write outside the release-please job (%q)", filepath.Base(f), i+1, job)
			}
		}
	}
}
