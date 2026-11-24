package domain

// What kravis schema reads: the declarations the rest of the library uses,
// exposed once so the schema never restates them.

// ErrorCodes lists the error codes a command can return, as docs/cli.md
// does; the CLI maps each to an exit code.
func ErrorCodes() []string {
	return []string{
		CodeInternal, CodeMissingActor, CodeActorNotLive, CodeUnknownField, CodeInvalidValue, CodeHolderMismatch,
		CodeNotFound, CodeVersionConflict, CodeDedupConflict, CodeReferenced, CodeWorkspaceExists, CodeStorageUnavailable,
	}
}

// InputFor returns an empty create input of the named record type, whose
// tags describe its fields; nil for an unknown type.
func InputFor(name string) any {
	switch name {
	case "user":
		return UserInput{}
	case "organization":
		return OrganizationInput{}
	case "person":
		return PersonInput{}
	case "interaction":
		return InteractionInput{}
	case "task":
		return TaskInput{}
	}
	return nil
}

// Defaults are the values a type uses when a field is omitted, by field
// name. A task starts in initialStatus, an interaction is outbound, and a
// task made from an interaction has the default purpose.
func Defaults(name string) map[string]any {
	switch name {
	case "interaction":
		return map[string]any{"direction": DirectionOutbound}
	case "task":
		return map[string]any{"status": initialStatus, "purpose": defaultPurpose}
	}
	return nil
}

// Directions are the values of an interaction's direction.
func Directions() []string { return append([]string(nil), directions...) }

// Kinds are the values of a user's kind.
func Kinds() []string { return []string{KindHuman, KindAgent} }

// ProviderDomains are the email providers, never an organization's domain.
func ProviderDomains() []string { return append([]string(nil), providerDomains...) }
