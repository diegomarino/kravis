package domain

import "fmt"

// Error codes, as docs/cli.md lists them. The domain returns an *Error; the
// CLI maps its code to an exit code in one place.
const (
	CodeInternal           = "INTERNAL"
	CodeMissingActor       = "MISSING_ACTOR"
	CodeActorNotLive       = "ACTOR_NOT_LIVE"
	CodeUnknownField       = "UNKNOWN_FIELD"
	CodeInvalidValue       = "INVALID_VALUE"
	CodeHolderMismatch     = "HOLDER_MISMATCH"
	CodeNotFound           = "NOT_FOUND"
	CodeVersionConflict    = "VERSION_CONFLICT"
	CodeDedupConflict      = "DEDUP_CONFLICT"
	CodeReferenced         = "REFERENCED"
	CodeWorkspaceExists    = "WORKSPACE_EXISTS"
	CodeStorageUnavailable = "STORAGE_UNAVAILABLE"
)

// Error is a failure with a stable code, structured details and, when the
// next step is not obvious from the details, a hint.
type Error struct {
	Code    string
	Message string
	Details map[string]any
	Hint    string
	// Cause is the underlying error, kept for errors.Is and errors.As.
	Cause error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Cause }

// newError builds an Error; details are key, value pairs.
func newError(code, hint, format string, args ...any) *Error {
	return &Error{Code: code, Hint: hint, Message: fmt.Sprintf(format, args...)}
}

func (e *Error) with(key string, value any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = value
	return e
}

// invalidValue is an INVALID_VALUE about one input field.
func invalidValue(field, hint, format string, args ...any) *Error {
	return newError(CodeInvalidValue, hint, format, args...).with("field", field)
}

// UsageError is an INVALID_VALUE about how the command was called.
func UsageError(message, hint string) *Error {
	return &Error{Code: CodeInvalidValue, Message: message, Hint: hint}
}

// InvalidFlag is an INVALID_VALUE about a flag, listing the valid values.
func InvalidFlag(flag string, allowed []string, format string, args ...any) *Error {
	e := newError(CodeInvalidValue, "", format, args...).with("field", flag)
	if allowed != nil {
		_ = e.with("allowed", allowed)
	}
	return e
}
