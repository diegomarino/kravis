// Package storage keeps a workspace's records, as docs/storage.md describes.
//
// Every operation goes through a Connector, which has exactly the five calls
// of an object store, so the same contract can later run on an S3-compatible
// bucket. Paths are slash-separated and relative to the workspace; nothing
// outside a connector assumes POSIX. On top of the connector, Store holds the
// record rules: the record format, the update loop, deduplication keys,
// sidecars and deletes.
package storage

import (
	"errors"
	"fmt"
	"io/fs"
)

// Token is an object's version token, as get returns it. It is opaque: only
// the connector that issued it can compare it.
type Token string

// Connector is the storage adapter: the only way records reach storage.
type Connector interface {
	// Get returns the object's content and its version token.
	Get(path string) ([]byte, Token, error)
	// Create writes the object only if the path does not exist (ErrExists).
	Create(path string, data []byte) error
	// Replace writes the object only if its token still matches
	// (ErrTokenMismatch); a missing object is ErrNotFound.
	Replace(path string, data []byte, token Token) error
	// Delete removes the object only if its token still matches.
	Delete(path string, token Token) error
	// List returns the paths under a prefix, sorted. The prefix is a plain
	// string prefix, as on an object store: "keys/" or "records/people/pe_".
	List(prefix string) ([]string, error)
}

// The connector's errors. Every error a connector returns wraps one of them.
var (
	ErrExists        = errors.New("already exists")
	ErrTokenMismatch = errors.New("version token does not match")
	ErrNotFound      = errors.New("not found")
	ErrUnavailable   = errors.New("storage unavailable")
)

// Store errors, on top of the connector's.
var (
	// ErrVersionConflict: the record kept changing during every attempt
	// (VERSION_CONFLICT).
	ErrVersionConflict = errors.New("the record kept changing")
	// ErrKeyConflict: a deduplication key already belongs to another
	// record (DEDUP_CONFLICT).
	ErrKeyConflict = errors.New("the deduplication key belongs to another record")
	// ErrIDChanged: an update tried to change a record's ID.
	ErrIDChanged = errors.New("a record's ID never changes")
)

// fail wraps err with the call and the path it was for.
func fail(op, path string, err error) error {
	return fmt.Errorf("%s %s: %w", op, path, err)
}

// validPath refuses absolute paths, "..", "." and empty elements, so no path
// leaves the workspace.
func validPath(op, path string) error {
	if !fs.ValidPath(path) || path == "." {
		return fail(op, path, errors.New("invalid path"))
	}
	return nil
}
