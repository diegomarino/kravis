package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPath(t *testing.T) {
	for _, tt := range []struct {
		name                     string
		goos, xdg, appData, home string
		want                     string
	}{
		{"macOS default", "darwin", "", "", "/h", "/h/.config/kravis/config.json"},
		{"linux default", "linux", "", "", "/h", "/h/.config/kravis/config.json"},
		{"xdg wins", "darwin", "/x", "", "/h", "/x/kravis/config.json"},
		{"windows appdata", "windows", "/x", "/a", "/h", "/a/kravis/config.json"},
		{"windows without appdata", "windows", "", "", "/h", "/h/AppData/Roaming/kravis/config.json"},
	} {
		got, err := configPath(tt.goos, tt.xdg, tt.appData, tt.home)
		if err != nil || filepath.ToSlash(got) != tt.want {
			t.Errorf("%s: %q, %v, want %q", tt.name, got, err, tt.want)
		}
	}
	if _, err := configPath("linux", "", "", ""); err == nil {
		t.Error("no folder at all must fail")
	}
}

func TestFindUpwardsAndEnvironment(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := Find(deep, ""); ok {
		t.Skip("the temp folder is inside a workspace")
	}
	if err := os.WriteFile(filepath.Join(root, "kravis.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, source, ok := Find(deep, "")
	if !ok || source != "search" || !sameDir(got, root) {
		t.Errorf("Find = %q %q %v", got, source, ok)
	}
	got, source, ok = Find(deep, "/some/folder")
	if !ok || source != "environment" || got != filepath.FromSlash("/some/folder") && filepath.Base(got) != "folder" {
		t.Errorf("Find with environment = %q %q %v", got, source, ok)
	}
}

func sameDir(a, b string) bool {
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra == rb
}
