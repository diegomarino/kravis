package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"
)

// Base holds what every record has: its ID and its metadata. A record type
// embeds it and lists its domain fields in schema order, with omitempty (or
// omitzero) on every optional one:
//
//	type Task struct {
//		storage.Base
//		Holder string `json:"holder"`
//		Title  string `json:"title"`
//		Due    string `json:"due,omitempty"`
//	}
//
// Encode writes `id` first, the domain fields in their order, then the
// metadata, wherever Base is embedded.
type Base struct {
	ID        string    `json:"id"`
	Version   int       `json:"version,omitzero"`
	CreatedAt time.Time `json:"created_at,omitzero"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	UpdatedBy string    `json:"updated_by,omitempty"`

	// Unknown holds the fields found on disk that the type does not
	// define; Encode writes them back.
	Unknown map[string]json.RawMessage `json:"-"`
	// Text is the sidecar's content. Nil leaves the sidecar as it is; a
	// write with an empty string removes it.
	Text *string `json:"-"`

	// disk holds the defined fields as found on disk, so Encode can write
	// back the unknown members of their nested objects.
	disk map[string]json.RawMessage
}

// Identified is a nested object of a list that has a domain identity: the
// unknown members found on disk follow the entry with the same identity,
// wherever the list moves it.
type Identified interface {
	Identity() string
}

func (b *Base) base() *Base { return b }

// Meta returns the record's ID and metadata, for code that handles any
// record type.
func (b *Base) Meta() *Base { return b }

// Record is a type that embeds Base.
type Record interface {
	base() *Base
	Meta() *Base
}

// metaKeys are written last, in Base's order.
var metaKeys = []string{"version", "created_at", "created_by", "updated_at", "updated_by"}

// IDs is a list of references, written sorted by ID.
type IDs []string

// MarshalJSON writes the IDs sorted, without changing the list.
func (ids IDs) MarshalJSON() ([]byte, error) {
	return marshal([]string(slices.Sorted(slices.Values(ids))))
}

// marshal is json.Marshal without HTML escaping and without the newline.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

type field struct {
	key string
	val json.RawMessage
}

// Encode writes a record in the disk format: two-space indentation, one field
// per line, UTF-8 unescaped, a trailing newline, empty fields omitted, `id`
// first, the domain fields, the unknown fields kept from disk sorted by
// name, and the metadata last.
func Encode(rec Record) ([]byte, error) {
	fields, err := ordered(rec)
	if err != nil {
		return nil, err
	}
	return writeObject(fields)
}

// Field is one field of a record as it is stored: its JSON name and value.
type Field struct {
	Name  string
	Value json.RawMessage
}

// Fields returns the record's fields in the disk order of Encode, with
// empty fields omitted.
func Fields(rec Record) ([]Field, error) {
	fields, err := ordered(rec)
	out := make([]Field, len(fields))
	for i, f := range fields {
		out[i] = Field{f.key, f.val}
	}
	return out, err
}

// FieldNames lists the JSON names the record type defines, metadata
// included, in declaration order.
func FieldNames(rec Record) []string {
	names := jsonNames(reflect.TypeOf(rec))
	// Base comes first in the struct but its metadata is written last.
	return slices.Concat(names[:1], slices.DeleteFunc(slices.Clone(names[1:]), func(n string) bool {
		return slices.Contains(metaKeys, n)
	}), metaKeys)
}

func ordered(rec Record) ([]field, error) {
	raw, err := marshal(rec)
	if err != nil {
		return nil, err
	}
	fields, err := objectFields(raw)
	if err != nil {
		return nil, err
	}
	types := fieldTypes(reflect.TypeOf(rec))
	var head, body, tail []field
	for _, f := range fields {
		if old, ok := rec.base().disk[f.key]; ok {
			if f.val, err = keepNested(f.val, old, types[f.key]); err != nil {
				return nil, fmt.Errorf("field %q: %w", f.key, err)
			}
		}
		switch {
		case f.key == "id":
			head = append(head, f)
		case slices.Contains(metaKeys, f.key):
			tail = append(tail, f)
		default:
			body = append(body, f)
		}
	}
	unknown := rec.base().Unknown
	for _, k := range slices.Sorted(maps.Keys(unknown)) {
		if slices.ContainsFunc(fields, func(f field) bool { return f.key == k }) {
			continue // a defined field wins
		}
		v, err := normalize(unknown[k])
		if err != nil {
			return nil, fmt.Errorf("unknown field %q: %w", k, err)
		}
		body = append(body, field{k, v})
	}
	return slices.Concat(head, body, tail), nil
}

// objectFields splits a JSON object into its fields, in order.
func objectFields(raw []byte) ([]field, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("a record must be a JSON object")
	}
	var fields []field
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		fields = append(fields, field{t.(string), v})
	}
	return fields, nil
}

// normalize rewrites a value read from disk without escapes, keeping numbers
// exactly as written.
func normalize(raw json.RawMessage) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return marshal(v)
}

func writeObject(fields []field) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, f := range fields {
		key, err := marshal(f.key)
		if err != nil {
			return nil, err
		}
		buf.WriteString("  ")
		buf.Write(key)
		buf.WriteString(": ")
		if err := json.Indent(&buf, f.val, "  ", "  "); err != nil {
			return nil, err
		}
		if i < len(fields)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// Decode reads a record from disk tolerantly: fields the type does not define
// are kept in Unknown, so the next Encode writes them back.
func Decode(data []byte, rec Record) error {
	if err := json.Unmarshal(data, rec); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	disk := map[string]json.RawMessage{}
	for _, k := range jsonNames(reflect.TypeOf(rec)) {
		if v, ok := all[k]; ok {
			disk[k] = v
		}
		delete(all, k)
	}
	if len(all) == 0 {
		all = nil
	}
	rec.base().Unknown, rec.base().disk = all, disk
	return nil
}

// keepNested adds to cur, the value of a defined field about to be written,
// the members that the value on disk has and the type of a nested object
// does not define, at any depth: in an object, into the same object; in a
// list of Identified entries, into the entry with the same identity. Members
// the type defines are always cur's, so a cleared field stays cleared.
func keepNested(cur, disk json.RawMessage, t reflect.Type) (json.RawMessage, error) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == nil:
		return cur, nil
	case t.Kind() == reflect.Struct && t != reflect.TypeFor[time.Time]():
		return keepObject(cur, disk, t)
	case t.Kind() == reflect.Slice && reflect.PointerTo(t.Elem()).Implements(reflect.TypeFor[Identified]()):
		var curs, disks []json.RawMessage
		if json.Unmarshal(cur, &curs) != nil || json.Unmarshal(disk, &disks) != nil {
			return cur, nil
		}
		byID := map[string]json.RawMessage{}
		for _, d := range disks {
			if id, ok := identity(d, t.Elem()); ok {
				byID[id] = d
			}
		}
		for i, c := range curs {
			id, ok := identity(c, t.Elem())
			if d, found := byID[id]; ok && found {
				v, err := keepNested(c, d, t.Elem())
				if err != nil {
					return nil, err
				}
				curs[i] = v
			}
		}
		return marshal(curs)
	}
	return cur, nil
}

func keepObject(cur, disk json.RawMessage, t reflect.Type) (json.RawMessage, error) {
	curs, err := objectFields(cur)
	if err != nil {
		return cur, nil
	}
	disks, err := objectFields(disk)
	if err != nil {
		return cur, nil // not an object on disk: nothing to keep
	}
	types := fieldTypes(t)
	var extra []field
	for _, d := range disks {
		i := slices.IndexFunc(curs, func(c field) bool { return c.key == d.key })
		typ, defined := types[d.key]
		switch {
		case defined && i >= 0:
			if curs[i].val, err = keepNested(curs[i].val, d.val, typ); err != nil {
				return nil, err
			}
		case !defined:
			v, err := normalize(d.val)
			if err != nil {
				return nil, fmt.Errorf("unknown field %q: %w", d.key, err)
			}
			extra = append(extra, field{d.key, v})
		}
	}
	slices.SortFunc(extra, func(a, b field) int { return strings.Compare(a.key, b.key) })
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range slices.Concat(curs, extra) {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := marshal(f.key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(f.val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// identity decodes a list entry as t and returns its identity.
func identity(raw json.RawMessage, t reflect.Type) (string, bool) {
	v := reflect.New(t)
	if json.Unmarshal(raw, v.Interface()) != nil {
		return "", false
	}
	return v.Interface().(Identified).Identity(), true
}

// DecodeStrict reads agent input: an unknown field or anything after the
// object is an error.
func DecodeStrict(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("unexpected data after the JSON object")
	}
	return nil
}

// jsonNames lists the JSON names a struct type defines, embedded structs
// included.
func jsonNames(t reflect.Type) []string {
	var names []string
	for _, f := range jsonFields(t) {
		names = append(names, f.Name)
	}
	return names
}

// fieldTypes maps the JSON names a struct type defines to their types.
func fieldTypes(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for _, f := range jsonFields(t) {
		out[f.Name] = f.Type
	}
	return out
}

// jsonFields lists the fields a struct type defines, embedded structs
// included, each with its JSON name as Name.
func jsonFields(t reflect.Type) []reflect.StructField {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var names []reflect.StructField
	for f := range t.Fields() {
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			names = append(names, jsonFields(f.Type)...)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name != "" {
			f.Name = name
		}
		names = append(names, f)
	}
	return names
}
