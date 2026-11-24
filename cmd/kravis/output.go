package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
	"github.com/diegomarino/kravis/internal/storage"
)

// Exit codes, as in docs/cli.md.
const (
	exitOK         = 0
	exitInternal   = 1
	exitUsage      = 2
	exitNotFound   = 3
	exitConflict   = 4
	exitStorage    = 5
	exitCheckFound = 6
)

// exitCodes and retryable are the one place that maps an error code to what
// the CLI does with it.
var exitCodes = map[string]int{
	domain.CodeInternal:           exitInternal,
	domain.CodeMissingActor:       exitUsage,
	domain.CodeActorNotLive:       exitUsage,
	domain.CodeUnknownField:       exitUsage,
	domain.CodeInvalidValue:       exitUsage,
	domain.CodeHolderMismatch:     exitUsage,
	domain.CodeWorkspaceExists:    exitUsage,
	domain.CodeNotFound:           exitNotFound,
	domain.CodeVersionConflict:    exitConflict,
	domain.CodeDedupConflict:      exitConflict,
	domain.CodeReferenced:         exitConflict,
	domain.CodeStorageUnavailable: exitStorage,
}

var retryable = []string{domain.CodeVersionConflict, domain.CodeStorageUnavailable}

// reply is what a command returns on success: the envelope of docs/cli.md.
type reply struct {
	Data     any
	Outcome  string
	Warnings []domain.Warning
	Filters  any
	// List marks a list, which always has total and next_cursor (null on
	// the last page).
	List       bool
	Total      int
	NextCursor *string
}

// MarshalJSON writes the envelope: data first, then only what applies.
func (r reply) MarshalJSON() ([]byte, error) {
	type field struct {
		key string
		val any
	}
	fields := []field{{"data", r.Data}}
	if r.Outcome != "" {
		fields = append(fields, field{"outcome", r.Outcome})
	}
	if len(r.Warnings) > 0 {
		fields = append(fields, field{"warnings", r.Warnings})
	}
	if r.Filters != nil {
		fields = append(fields, field{"filters", r.Filters})
	}
	if r.List {
		fields = append(fields, field{"total", r.Total}, field{"next_cursor", r.NextCursor})
	}
	var buf strings.Builder
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		var b strings.Builder
		if err := writeJSON(&b, f.val); err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, "%q:%s", f.key, strings.TrimSuffix(b.String(), "\n"))
	}
	buf.WriteByte('}')
	return []byte(buf.String()), nil
}

// errorBody is the error object of docs/cli.md.
type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
	Retryable bool           `json:"retryable"`
	Hint      string         `json:"hint,omitempty"`
}

// toError maps any error to a domain error: typed errors keep their code,
// the storage layer's sentinels get theirs, and anything else is INTERNAL.
func toError(err error) *domain.Error {
	var de *domain.Error
	switch {
	case errors.As(err, &de):
		return de
	case errors.Is(err, storage.ErrNotFound):
		return &domain.Error{Code: domain.CodeNotFound, Message: err.Error(), Cause: err}
	case errors.Is(err, storage.ErrUnavailable):
		return &domain.Error{Code: domain.CodeStorageUnavailable, Message: err.Error(), Cause: err,
			Hint: "check that the workspace folder and the local configuration are readable and writable"}
	case errors.Is(err, storage.ErrVersionConflict):
		return &domain.Error{Code: domain.CodeVersionConflict, Message: err.Error(), Cause: err}
	case errors.Is(err, storage.ErrKeyConflict):
		return &domain.Error{Code: domain.CodeDedupConflict, Message: err.Error(), Cause: err}
	}
	return &domain.Error{Code: domain.CodeInternal, Message: err.Error(), Cause: err}
}

// writeError prints the error object on stderr and returns the exit code.
func writeError(w io.Writer, err error) int {
	de := toError(err)
	code, ok := exitCodes[de.Code]
	if !ok {
		code = exitInternal
	}
	body := errorBody{
		Code: de.Code, Message: de.Message, Details: de.Details,
		Retryable: slices.Contains(retryable, de.Code), Hint: de.Hint,
	}
	if body.Details == nil {
		body.Details = map[string]any{}
	}
	_ = writeJSON(w, map[string]any{"error": body})
	return code
}

// writeJSON prints one compact JSON object on one line, without HTML
// escaping, so `<user>` in a hint stays readable.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// nonNull turns a nil object or list into an empty one, so data is never
// null.
func nonNull(v any) any {
	rv := reflect.ValueOf(v)
	switch {
	case !rv.IsValid():
		return map[string]any{}
	case rv.Kind() == reflect.Slice && rv.IsNil():
		return reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	case rv.Kind() == reflect.Map && rv.IsNil():
		return reflect.MakeMap(rv.Type()).Interface()
	case rv.Kind() == reflect.Pointer && rv.IsNil():
		return map[string]any{}
	}
	return v
}

// emit prints a success on stdout: JSON outside a terminal or when forced,
// a table for people on one.
func (a *app) emit(cmd *cobra.Command, r reply) error {
	r.Data = nonNull(r.Data)
	if a.output != "json" && a.isTTY(a.stdout) {
		return writeTable(a.stdout, r)
	}
	return writeJSON(a.stdout, r)
}

// writeTable prints a result as plain text: a record as key and value lines,
// a list as columns. It is meant for people and kept minimal.
func writeTable(w io.Writer, r reply) error {
	var generic any
	raw, err := json.Marshal(r.Data)
	if err == nil {
		err = json.Unmarshal(raw, &generic)
	}
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if r.Outcome != "" {
		_, _ = fmt.Fprintf(tw, "outcome\t%s\n", r.Outcome)
	}
	switch v := generic.(type) {
	case map[string]any:
		for _, k := range sortedKeys(v) {
			_, _ = fmt.Fprintf(tw, "%s\t%s\n", k, cell(v[k]))
		}
	case []any:
		all := map[string]any{} // every row's fields: a row may leave one out
		for _, row := range v {
			m, _ := row.(map[string]any)
			for k := range m {
				all[k] = nil
			}
		}
		cols := sortedKeys(all)
		_, _ = fmt.Fprintln(tw, strings.ToUpper(strings.Join(cols, "\t")))
		for _, row := range v {
			m, _ := row.(map[string]any)
			cells := make([]string, len(cols))
			for i, c := range cols {
				cells[i] = cell(m[c])
			}
			_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
		}
	}
	for _, wn := range r.Warnings {
		_, _ = fmt.Fprintf(tw, "warning\t%s: %s\n", wn.Code, wn.Message)
	}
	return tw.Flush()
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func cell(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
