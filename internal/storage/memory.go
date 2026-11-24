package storage

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Op names a connector call, for Memory's hook.
type Op string

// The five calls.
const (
	OpGet     Op = "get"
	OpCreate  Op = "create"
	OpReplace Op = "replace"
	OpDelete  Op = "delete"
	OpList    Op = "list"
)

// Memory is a connector in memory, for tests. Its Hook runs at the start of
// every call, before any check: it can change an object behind the caller's
// back with Set, to report a version change on demand, or return an error to
// make the call fail, as a crash or an outage would.
type Memory struct {
	// Hook, when set, runs before each call; a non-nil error is returned
	// as the call's result and the call does nothing.
	Hook func(op Op, path string) error

	mu   sync.Mutex
	objs map[string]memObject
	next int
}

type memObject struct {
	data  []byte
	token Token
}

// NewMemory returns an empty in-memory connector.
func NewMemory() *Memory { return &Memory{objs: map[string]memObject{}} }

func (m *Memory) hook(op Op, p string) error {
	if m.Hook == nil {
		return nil
	}
	return m.Hook(op, p)
}

// put stores data under a fresh token; the caller holds the lock.
func (m *Memory) put(p string, data []byte) {
	m.next++
	m.objs[p] = memObject{data: slices.Clone(data), token: Token("m" + strconv.Itoa(m.next))}
}

// Set writes an object unconditionally, as a concurrent writer would: its
// token changes even when the content does not.
func (m *Memory) Set(p string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.put(p, data)
}

// Snapshot returns every object's content by path, to compare what a test
// left behind byte for byte.
func (m *Memory) Snapshot() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.objs))
	for p, o := range m.objs {
		out[p] = string(o.data)
	}
	return out
}

// Get implements Connector.
func (m *Memory) Get(p string) ([]byte, Token, error) {
	if err := m.hook(OpGet, p); err != nil {
		return nil, "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[p]
	if !ok {
		return nil, "", fail("get", p, ErrNotFound)
	}
	return slices.Clone(o.data), o.token, nil
}

// Create implements Connector.
func (m *Memory) Create(p string, data []byte) error {
	if err := m.hook(OpCreate, p); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objs[p]; ok {
		return fail("create", p, ErrExists)
	}
	m.put(p, data)
	return nil
}

// Replace implements Connector.
func (m *Memory) Replace(p string, data []byte, token Token) error {
	if err := m.hook(OpReplace, p); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check("replace", p, token); err != nil {
		return err
	}
	m.put(p, data)
	return nil
}

// Delete implements Connector.
func (m *Memory) Delete(p string, token Token) error {
	if err := m.hook(OpDelete, p); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check("delete", p, token); err != nil {
		return err
	}
	delete(m.objs, p)
	return nil
}

func (m *Memory) check(op, p string, token Token) error {
	o, ok := m.objs[p]
	if !ok {
		return fail(op, p, ErrNotFound)
	}
	if o.token != token {
		return fail(op, p, ErrTokenMismatch)
	}
	return nil
}

// List implements Connector.
func (m *Memory) List(prefix string) ([]string, error) {
	if err := m.hook(OpList, prefix); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, p := range slices.Sorted(maps.Keys(m.objs)) {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out, nil
}
