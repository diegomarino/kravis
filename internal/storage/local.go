package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Local is the connector for a workspace on the local filesystem.
//
// It writes to a temporary file next to the target and moves it into place.
// A conditional write checks the version right before the rename and takes no
// lock: the window between the check and the rename lasts under a
// millisecond and is accepted (docs/storage.md, Writes and concurrency).
// The version token is the SHA-256 of the content and the file's
// modification time, so every write gives a new token, even of the same
// bytes.
type Local struct {
	root string
}

// NewLocal returns the connector for the workspace folder root.
func NewLocal(root string) *Local { return &Local{root: root} }

// tempMark is in every temporary file's name, after a leading dot, so List
// can skip them. No object's name starts with a dot.
const tempMark = ".tmp-"

func (l *Local) file(p string) string { return filepath.Join(l.root, filepath.FromSlash(p)) }

// read returns the file's content and token, both from one open file.
func (l *Local) read(p string) ([]byte, Token, error) {
	f, err := os.Open(l.file(p))
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, "", err
	}
	return data, tokenOf(data, info), nil
}

func tokenOf(data []byte, info fs.FileInfo) Token {
	sum := sha256.Sum256(data)
	return Token(hex.EncodeToString(sum[:]) + "-" + strconv.FormatInt(info.ModTime().UnixNano(), 36))
}

// ioErr maps a filesystem error to the connector's errors.
func ioErr(op, p string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fail(op, p, ErrNotFound)
	}
	return fail(op, p, errors.Join(ErrUnavailable, err))
}

// Get implements Connector.
func (l *Local) Get(p string) ([]byte, Token, error) {
	if err := validPath("get", p); err != nil {
		return nil, "", err
	}
	data, token, err := l.read(p)
	if err != nil {
		return nil, "", ioErr("get", p, err)
	}
	return data, token, nil
}

// Create implements Connector: the temporary file is linked into place,
// which fails if the target exists, so a reader never sees half a file.
func (l *Local) Create(p string, data []byte) (Token, error) {
	if err := validPath("create", p); err != nil {
		return "", err
	}
	tmp, written, err := l.writeTemp(p, data)
	if err != nil {
		return "", ioErr("create", p, err)
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := os.Link(tmp, l.file(p)); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fail("create", p, ErrExists)
		}
		return "", ioErr("create", p, err)
	}
	return written, nil
}

// Replace implements Connector.
func (l *Local) Replace(p string, data []byte, token Token) (Token, error) {
	if err := validPath("replace", p); err != nil {
		return "", err
	}
	tmp, written, err := l.writeTemp(p, data)
	if err != nil {
		return "", ioErr("replace", p, err)
	}
	defer func() { _ = os.Remove(tmp) }() // a no-op once renamed
	if err := l.check("replace", p, token); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, l.file(p)); err != nil {
		return "", ioErr("replace", p, err)
	}
	return written, nil
}

// Delete implements Connector.
func (l *Local) Delete(p string, token Token) error {
	if err := validPath("delete", p); err != nil {
		return err
	}
	if err := l.check("delete", p, token); err != nil {
		return err
	}
	if err := os.Remove(l.file(p)); err != nil {
		return ioErr("delete", p, err)
	}
	return nil
}

// check reads the current version and compares it with token.
func (l *Local) check(op, p string, token Token) error {
	_, current, err := l.read(p)
	if err != nil {
		return ioErr(op, p, err)
	}
	if current != token {
		return fail(op, p, ErrTokenMismatch)
	}
	return nil
}

// writeTemp writes data to a new temporary file in the target's folder,
// creating the folder, and returns its name and the token it will have in
// place: a link or a rename keeps its modification time.
func (l *Local) writeTemp(p string, data []byte) (string, Token, error) {
	dir := filepath.Dir(l.file(p))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	f, err := os.CreateTemp(dir, "."+path.Base(p)+tempMark+"*")
	if err != nil {
		return "", "", err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	var info fs.FileInfo
	if err == nil {
		info, err = f.Stat()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", "", err
	}
	return f.Name(), tokenOf(data, info), nil
}

// List implements Connector. A prefix whose folder does not exist lists
// nothing.
func (l *Local) List(prefix string) ([]string, error) {
	dir := ""
	if i := strings.LastIndex(prefix, "/"); i >= 0 {
		dir = prefix[:i]
	}
	if dir != "" {
		if err := validPath("list", dir); err != nil {
			return nil, err
		}
	}
	start := l.file(dir)
	var paths []string
	err := filepath.WalkDir(start, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && name == start {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() || (strings.HasPrefix(d.Name(), ".") && strings.Contains(d.Name(), tempMark)) {
			return nil
		}
		rel, err := filepath.Rel(l.root, name)
		if err != nil {
			return err
		}
		if rel = filepath.ToSlash(rel); strings.HasPrefix(rel, prefix) {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return nil, ioErr("list", prefix, err)
	}
	sort.Strings(paths)
	return paths, nil
}
