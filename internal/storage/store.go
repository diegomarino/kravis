package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// Attempts is how many times an update reads, applies and writes before it
// gives up with ErrVersionConflict.
const Attempts = 3

// Store keeps records on a connector: their format, metadata, sidecars and
// deduplication keys. It knows paths, not record types: the caller computes
// a record's path from its ID.
type Store struct {
	c   Connector
	now func() time.Time
}

// NewStore returns a store on c. now gives the time of each write; nil means
// time.Now.
func NewStore(c Connector, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{c: c, now: now}
}

func (s *Store) stamp() time.Time { return s.now().UTC().Truncate(time.Second) }

// Key is a natural key: a kind (person-email, organization-domain,
// interaction-ref, task-origin) and its value, such as
// "interaction|org_petalo|email:message:<message-id>".
type Key struct {
	Kind  string
	Value string
}

// Path is where the key's file lives: keys/<kind>/<h0h1>/<sha256>.json.
func (k Key) Path() string {
	sum := sha256.Sum256([]byte(k.Value))
	h := hex.EncodeToString(sum[:])
	return "keys/" + k.Kind + "/" + h[:2] + "/" + h + ".json"
}

// keyFile is a key file's content: the key and the record that holds it.
type keyFile struct {
	Key    string `json:"key"`
	Record string `json:"record"`
}

// SidecarPath is the Markdown file next to a record's JSON file.
func SidecarPath(p string) string { return strings.TrimSuffix(p, ".json") + ".md" }

// Load reads the record at p into rec and returns its version token, for a
// later Remove. The sidecar is not read: see Text.
func (s *Store) Load(p string, rec Record) (Token, error) {
	data, token, err := s.c.Get(p)
	if err != nil {
		return "", err
	}
	if err := Decode(data, rec); err != nil {
		return "", fail("decode", p, err)
	}
	return token, nil
}

// Text reads the sidecar of the record at p; no sidecar is no text.
func (s *Store) Text(p string) (string, error) {
	data, _, err := s.c.Get(SidecarPath(p))
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return string(data), err
}

// List returns the paths under prefix, as the connector does.
func (s *Store) List(prefix string) ([]string, error) { return s.c.List(prefix) }

// Create writes a new record whose ID the caller allocated, at pathOf(ID),
// with its natural keys, in the three steps of docs/storage.md: the ID, then
// each key with create, then the record, its sidecar first.
//
// When a key already belongs to a record that exists, nothing is written
// and Create returns that record's ID with existing true; the caller decides
// whether the data sent contradicts it. When a key's record is missing,
// because an earlier attempt crashed before writing it, Create takes the ID
// stored in the key and completes that record instead of duplicating it.
// A record already at the path is ErrExists. The record's metadata is set
// here: version 1, created and updated by actor, now.
func (s *Store) Create(rec Record, actor string, pathOf func(id string) string, keys []Key) (id string, existing bool, err error) {
	b := rec.base()
	adopted := false
	for _, k := range keys {
		owner, err := s.keyOwner(k)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		if found, err := s.exists(pathOf(owner)); err != nil || found {
			return owner, found, err
		}
		if adopted && owner != b.ID {
			return "", false, fail("create", k.Path(), ErrKeyConflict)
		}
		b.ID, adopted = owner, true
	}
	p := pathOf(b.ID)
	if found, err := s.exists(p); err != nil {
		return "", false, err
	} else if found {
		return "", false, fail("create", p, ErrExists)
	}

	created, owner, err := s.addKeys(keys, b.ID)
	if err != nil {
		err = errors.Join(err, s.dropKeys(created, b.ID))
		if owner != "" {
			if found, ferr := s.exists(pathOf(owner)); ferr != nil || found {
				return owner, found, ferr
			}
		}
		return "", false, err
	}

	now := s.stamp()
	b.Version, b.CreatedAt, b.CreatedBy, b.UpdatedAt, b.UpdatedBy = 1, now, actor, now, actor
	undo, err := s.writeText(p, b.Text)
	var data []byte
	if err == nil {
		data, err = Encode(rec)
	}
	if err == nil {
		_, err = s.c.Create(p, data)
	}
	if err != nil {
		return "", false, errors.Join(err, undo(), s.dropKeys(created, b.ID))
	}
	return b.ID, false, nil
}

// Update reads the record at p, applies the change and writes it with
// replace, conditional on the token it read. If the record changed in
// between, it rereads and reapplies, up to Attempts times, then fails with
// ErrVersionConflict and writes nothing more.
//
// keys, when not nil, gives a record's natural keys. Keys the change adds are
// created first, and one that belongs to another record fails the update
// with ErrKeyConflict before the record is written; keys it removes are
// deleted after. The record's version goes up by one, and updated_at and
// updated_by are set; its ID and creation fields cannot change.
func Update[T any, P interface {
	*T
	Record
}](s *Store, p, actor string, apply func(P) error, keys func(P) []Key) (P, error) {
	if keys == nil {
		keys = func(P) []Key { return nil }
	}
	for range Attempts {
		data, token, err := s.c.Get(p)
		if err != nil {
			return nil, err
		}
		old, cur := P(new(T)), P(new(T))
		if err := Decode(data, old); err != nil {
			return nil, fail("decode", p, err)
		}
		_ = Decode(data, cur) // the same bytes decoded above
		if err := apply(cur); err != nil {
			return nil, err
		}
		ob, cb := old.base(), cur.base()
		if cb.ID != ob.ID {
			return nil, fail("update", p, ErrIDChanged)
		}
		cb.Version, cb.CreatedAt, cb.CreatedBy = ob.Version+1, ob.CreatedAt, ob.CreatedBy
		cb.UpdatedAt, cb.UpdatedBy = s.stamp(), actor

		before, after := keys(old), keys(cur)
		added := slices.DeleteFunc(slices.Clone(after), func(k Key) bool { return slices.Contains(before, k) })
		removed := slices.DeleteFunc(slices.Clone(before), func(k Key) bool { return slices.Contains(after, k) })
		// release deletes the keys this attempt created, unless the record
		// on disk holds them now: a concurrent update may have committed them.
		release := func(created []Key) error {
			if len(created) == 0 {
				return nil
			}
			data, _, err := s.c.Get(p)
			if err != nil {
				return err
			}
			disk := P(new(T))
			if err := Decode(data, disk); err != nil {
				return fail("decode", p, err)
			}
			held := keys(disk)
			return s.dropKeys(slices.DeleteFunc(created, func(k Key) bool { return slices.Contains(held, k) }), cb.ID)
		}
		created, owner, err := s.addKeys(added, cb.ID)
		if owner != "" {
			return nil, errors.Join(err, release(created))
		}
		undo := func() error { return nil }
		if err == nil {
			undo, err = s.writeText(p, cb.Text)
		}
		if err == nil {
			if data, err = Encode(cur); err == nil {
				_, err = s.c.Replace(p, data, token)
			}
		}
		if err != nil {
			err = errors.Join(err, undo(), release(created))
			if errors.Is(err, ErrTokenMismatch) || errors.Is(err, ErrExists) {
				continue // changed in between: reread and reapply
			}
			return nil, err
		}
		return cur, s.dropKeys(removed, cb.ID)
	}
	return nil, fail("update", p, ErrVersionConflict)
}

// Remove deletes the record at p, conditional on the token Load returned,
// then its sidecar, then the keys that still point to it. The caller checks
// first that nothing refers to the record. A record that changed since it
// was read is ErrVersionConflict, and then nothing is removed.
func (s *Store) Remove(p, id string, token Token, keys []Key) error {
	if err := s.c.Delete(p, token); err != nil {
		if errors.Is(err, ErrTokenMismatch) {
			return fail("delete", p, ErrVersionConflict)
		}
		return err
	}
	none := ""
	_, err := s.writeText(p, &none)
	return errors.Join(err, s.dropKeys(keys, id))
}

func (s *Store) exists(p string) (bool, error) {
	_, _, err := s.c.Get(p)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) keyOwner(k Key) (string, error) {
	owner, _, err := s.readKey(k)
	return owner, err
}

func (s *Store) readKey(k Key) (string, Token, error) {
	data, token, err := s.c.Get(k.Path())
	if err != nil {
		return "", "", err
	}
	var f keyFile
	if err := json.Unmarshal(data, &f); err != nil {
		return "", "", fail("decode", k.Path(), err)
	}
	return f.Record, token, nil
}

// addKeys creates each key pointing to id. A key that already points to id
// is kept. A key that points to another record stops it with
// ErrKeyConflict, and that record's ID is returned as owner. On an error,
// created lists the keys it created, for the caller to delete again.
func (s *Store) addKeys(keys []Key, id string) (created []Key, owner string, err error) {
	for _, k := range keys {
		var data []byte
		data, err = writeObject([]field{{"key", mustMarshal(k.Value)}, {"record", mustMarshal(id)}})
		if err == nil {
			_, err = s.c.Create(k.Path(), data)
		}
		if errors.Is(err, ErrExists) {
			if owner, err = s.keyOwner(k); err == nil && owner == id {
				owner = ""
				continue
			}
			if err == nil {
				err = fail("create", k.Path(), ErrKeyConflict)
			}
		}
		if err != nil {
			return created, owner, err
		}
		created = append(created, k)
	}
	return created, "", nil
}

// dropKeys deletes the keys that point to id; keys that are gone or point
// elsewhere are left alone.
func (s *Store) dropKeys(keys []Key, id string) error {
	var errs []error
	for _, k := range keys {
		owner, token, err := s.readKey(k)
		if errors.Is(err, ErrNotFound) || (err == nil && owner != id) {
			continue
		}
		if err == nil {
			err = s.c.Delete(k.Path(), token)
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// writeText writes the sidecar of the record at p: nil leaves it alone, an
// empty text removes it. undo puts the earlier sidecar back, unless someone
// wrote it since: its token is no longer the one this write returned.
func (s *Store) writeText(p string, text *string) (undo func() error, err error) {
	undo = func() error { return nil }
	if text == nil {
		return undo, nil
	}
	sp := SidecarPath(p)
	old, token, err := s.c.Get(sp)
	var ours Token // "" when this write removed the sidecar
	switch {
	case errors.Is(err, ErrNotFound):
		if *text == "" {
			return undo, nil
		}
		old = nil
		ours, err = s.c.Create(sp, []byte(*text))
	case err != nil:
		return undo, err
	case *text == "":
		err = s.c.Delete(sp, token)
	default:
		// Written even when unchanged, so its token says this write was last.
		ours, err = s.c.Replace(sp, []byte(*text), token)
	}
	if err != nil {
		return undo, err
	}
	return func() error { return s.restoreText(sp, ours, old) }, nil
}

// restoreText puts old back at sp (nil: no sidecar) if sp is still at the
// token ours, or still absent when ours is "" (this write removed it).
func (s *Store) restoreText(sp string, ours Token, old []byte) error {
	_, token, err := s.c.Get(sp)
	switch {
	case errors.Is(err, ErrNotFound):
		if ours != "" || old == nil {
			return nil
		}
		_, err = s.c.Create(sp, old)
		return err
	case err != nil:
		return err
	case ours == "" || token != ours:
		return nil // someone else wrote it since
	case old == nil:
		return s.c.Delete(sp, token)
	}
	_, err = s.c.Replace(sp, old, token)
	return err
}

func mustMarshal(s string) json.RawMessage {
	b, _ := marshal(s) // a string always marshals
	return b
}
