// Package workspace finds the workspace a command runs on and keeps the
// local configuration of the person on this machine.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/diegomarino/kravis/internal/storage"
)

// Find returns the folder of the workspace to use: envWorkspace
// (KRAVIS_WORKSPACE) when set, else the nearest folder from start upwards that
// holds kravis.json. ok is false outside any workspace. The returned
// source is "environment" or "search".
func Find(start, envWorkspace string) (root, source string, ok bool) {
	if envWorkspace != "" {
		if abs, err := filepath.Abs(envWorkspace); err == nil {
			return abs, "environment", true
		}
		return envWorkspace, "environment", true
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", "", false
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "kravis.json")); err == nil && !info.IsDir() {
			return dir, "search", true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

// ConfigPath is the local configuration file: $XDG_CONFIG_HOME/kravis/config.json,
// by default ~/.config/kravis/config.json, and %AppData%\kravis\config.json on
// Windows. It is never os.UserConfigDir, which points elsewhere on macOS.
func ConfigPath() (string, error) {
	home, _ := os.UserHomeDir()
	return configPath(runtime.GOOS, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("APPDATA"), home)
}

func configPath(goos, xdg, appData, home string) (string, error) {
	var dir string
	switch {
	case goos == "windows" && appData != "":
		dir = appData
	case goos != "windows" && xdg != "":
		dir = xdg
	case home != "":
		dir = filepath.Join(home, ".config")
		if goos == "windows" {
			dir = filepath.Join(home, "AppData", "Roaming")
		}
	default:
		return "", errors.New("cannot find the local configuration folder: set HOME or XDG_CONFIG_HOME")
	}
	return filepath.Join(dir, "kravis", "config.json"), nil
}

// localConfig is the local configuration: each workspace ID's default actor.
type localConfig struct {
	Workspaces map[string]entry `json:"workspaces"`
}

type entry struct {
	Actor string `json:"actor"`
}

func unavailable(path string, err error) error {
	return fmt.Errorf("local configuration %s: %w", path, errors.Join(storage.ErrUnavailable, err))
}

func read() (localConfig, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return localConfig{}, "", errors.Join(storage.ErrUnavailable, err)
	}
	var cfg localConfig
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, path, nil
	}
	if err != nil {
		return cfg, path, unavailable(path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, path, unavailable(path, err)
	}
	return cfg, path, nil
}

// DefaultActor returns the default actor stored for the workspace, or ""
// when there is none.
func DefaultActor(workspaceID string) (string, error) {
	cfg, _, err := read()
	return cfg.Workspaces[workspaceID].Actor, err
}

// SetDefaultActor stores the workspace's default actor and reports whether
// that changed the file. With onlyIfAbsent it keeps an actor that is already
// stored.
func SetDefaultActor(workspaceID, actor string, onlyIfAbsent bool) (changed bool, err error) {
	cfg, path, err := read()
	if err != nil {
		return false, err
	}
	have := cfg.Workspaces[workspaceID].Actor
	if have == actor || (onlyIfAbsent && have != "") {
		return false, nil
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]entry{}
	}
	cfg.Workspaces[workspaceID] = entry{Actor: actor}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, unavailable(path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return false, unavailable(path, err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return false, unavailable(path, werr)
	}
	return true, nil
}
