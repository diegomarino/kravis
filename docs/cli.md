# CLI contract

The contract of the `kravis` command for agents, scripts and people. This document covers increment 1 and notes what increment 2 adds. All domain logic lives in the Go library; the CLI is a thin layer over it, and a future MCP server will be another thin layer over the same library and schemas.

## Shape

```text
kravis <type> <verb> [arguments] [flags]
kravis init | whoami | schema [type] | check
```

- The core verbs, uniform across types, are `list`, `get`, `create`, `update` and `delete`.
- A transition that has rules gets its own verb next to its type, and `update` cannot bypass it. In increment 1 that is `task status`; increment 2 adds `deal stage`, `deal party add/remove` and `project close/reopen`.
- There are no composite operations. Recording an interaction and its follow-up takes two calls, and each one is safe to repeat.

## Workspace

- Kravis works on the workspace given by `KRAVIS_WORKSPACE`, or else on the nearest folder holding `kravis.json`, from the current folder upwards. Outside any workspace, every command except `init` fails with `STORAGE_UNAVAILABLE` and a hint.
- `init` creates the workspace in the current folder, or in `KRAVIS_WORKSPACE` when it is set.

## Identity and holder

- Every write needs an actor. Kravis takes it from `--actor <user>`, then `KRAVIS_ACTOR`, then the local configuration: `$XDG_CONFIG_HOME/kravis/config.json`, by default `~/.config/kravis/config.json` on macOS and Linux (not Go's `os.UserConfigDir`, which points to `~/Library/Application Support` on macOS) and `%AppData%\kravis\config.json` on Windows. It maps each workspace ID to its default actor, `{"workspaces": {"ws_…": {"actor": "us_…"}}}`. It belongs to a person on a machine, never to the workspace. A write without an actor fails with `MISSING_ACTOR` (exit code 2) and a hint naming both options. Reads need no actor.
- The actor must exist and be live. In the MVP the actor is declared rather than authenticated: it serves attribution and auditing, and it is not a security control.
- A work record (interaction, task, and later deal, project and note) takes its holder from the actor, so there is no `--holder` flag on writes.
- An actor can only write the work records of its own holder: an update, transition or delete of another holder's record returns `NOT_FOUND`, also with `--all-holders`, which only widens reads.
- A read made with a declared actor is limited to the actor's holder, and the response states that filter: a `list` returns only that holder's work records, and a `get` of another holder's record returns `NOT_FOUND`. `--all-holders` removes the limit. A read without an actor is not limited. People, organizations and users are shared and never filtered.
- `kravis whoami` prints the resolved actor, where it came from (flag, environment or configuration) and its holder. `kravis whoami --set <user>` stores a live user as this workspace's default actor in the local configuration.

## Output

- Outside a TTY, every command prints exactly one JSON object, compact and on one line, including `--version` and usage errors such as an unknown command or flag (exit code 2, nothing on stdout). Only `--help` prints text for people. On a TTY it prints tables meant for people; `KRAVIS_OUTPUT=json` or `--output json` forces JSON there.
- An unknown command is `INVALID_VALUE`; `UNKNOWN_FIELD` is only for an unknown flag or field.
- `kravis --version` prints `{"data":{"version":"0.2.0"}}`, without the leading `v`, and `dev` only when no version is known. The version is the one injected at build time, else the module version from the build info (`go install …@vX.Y.Z`), else `dev`.
- Data goes to stdout and errors to stderr, so an error is never mixed with data in a pipe.

Success:

```json
{
  "data": { "...": "..." },
  "outcome": "created",
  "warnings": [{ "code": "SIMILAR_EXISTS", "message": "…", "details": { "id": "pe_elena-ruiz" } }],
  "filters": { "holder": "org_petalo" }
}
```

- `data` is always present and never `null`: an empty list is `[]`.
- `warnings` appears only when there are warnings.
- `filters` appears only on reads, and holds only what Kravis applies by itself (the holder limit), never what the caller asked for.

- **Writes** (`create`, `update`, transitions, `delete`) return `outcome`:
  - `created` for a new record;
  - `existing` when a create found the same natural key and created nothing;
  - `updated` when something changed;
  - `unchanged` when nothing changed and the version was not bumped;
  - `deleted`.
  `data` holds the type's compact view plus the fields the write touched. `--fields a,b` and `--full` widen it. For `existing`, `unchanged` and `deleted` it holds the compact view (of the record as it was, for `deleted`), widened by `--fields` and `--full`.
- **`get`** returns the compact view. `--fields a,b` and `--full` widen it as they do for writes.
- **Lists** return the compact view of each record in `data`, an array. `total` and `next_cursor` sit beside `data`:
  - `total` counts the filtered set the caller sees, at the time of the call.
  - `next_cursor` is always present, and `null` on the last page. It is an opaque, stateless keyset cursor (the last sort value, the ID, and a fingerprint of the filters and order) that `--cursor <next_cursor>` continues. A cursor that is corrupt, or does not match the filters or order, is `INVALID_VALUE` with `details.field: "cursor"`.
  - At most 50 records come by default. `--limit <n>` changes it, and `--limit 0` returns them all; there is no maximum. A negative or non-numeric `--limit` is `INVALID_VALUE`, and so is `--limit 0` with `--cursor`.
  - A record whose own sort value crosses the cursor between two pages may be skipped or repeated.
  - Lists show only live records by default; `--all` or an explicit category includes the others.
- Each type has a fixed default order, declared in `kravis schema`, with the ID breaking ties:
  - users, organizations and people by name, compared as its slug (lowercase ASCII without accents, the same transformation that makes IDs), so the order is the same on every machine. A record without a name (a person with only an email, a user without a name) uses its email, else its ID, compared the same way;
  - interactions by `occurred_at`, newest first;
  - tasks by `due`, soonest first, with tasks without a due date last.

Error (stderr):

```json
{
  "error": {
    "code": "INVALID_VALUE",
    "message": "status 'pending' is not in the task status catalog",
    "details": { "field": "status", "allowed": ["todo", "in-progress", "waiting", "done", "canceled"] },
    "retryable": false,
    "hint": "use one of the allowed values"
  }
}
```

`message` and `hint` are in English. `hint` is present only when `details` does not make the next step obvious.

- A call reports one error. An error never carries `warnings`.
- `retryable` is `true` only for `VERSION_CONFLICT` and `STORAGE_UNAVAILABLE`.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success, including `existing` and `unchanged` |
| 1 | Internal failure |
| 2 | Usage or validation error |
| 3 | Not found |
| 4 | Conflict: version, or an incompatible deduplication |
| 5 | Storage unavailable or I/O error |
| 6 | `check` found issues: its findings are data on stdout, not an error |

### Error and warning codes (increment 1)

| Code | Kind | When |
|---|---|---|
| `INTERNAL` | error, 1 | An unexpected failure inside Kravis itself |
| `MISSING_ACTOR` | error, 2 | A write without an actor |
| `ACTOR_NOT_LIVE` | error, 2 | The actor does not exist or is not live |
| `UNKNOWN_FIELD` | error, 2 | Agent input with a field the schema does not define |
| `INVALID_VALUE` | error, 2 | A value outside its catalog or format, or an unknown command; `details.allowed` lists the valid values |
| `HOLDER_MISMATCH` | error, 2 | A link to a record of another holder |
| `NOT_FOUND` | error, 3 | The ID or alias does not exist |
| `VERSION_CONFLICT` | error, 4 | The record kept changing during 3 attempts |
| `DEDUP_CONFLICT` | error, 4 | The natural key exists with contradicting data, or identifying references are split across two interactions |
| `REFERENCED` | error, 4 | A delete of a record that others refer to; `details.referrers` lists them |
| `WORKSPACE_EXISTS` | error, 2 | `init` with data that does not match the existing workspace |
| `STORAGE_UNAVAILABLE` | error, 5 | The workspace cannot be read or written |
| `SIMILAR_EXISTS` | warning | A slug got a `-2` suffix; `details.id` names the existing record |
| `GENERIC_ADDRESS` | warning | A person was created from `info@`, `noreply@` or a similar address |
| `MULTIPLE_AFFILIATIONS` | warning | A participant person has several affiliations, so none was added |
| `UNKNOWN_SOURCE` | warning | A source reference to a source that is not in the catalog |

### Input

- Agent input is strict: unknown flags and fields are refused (`UNKNOWN_FIELD`). Records read from disk are tolerant instead: Kravis keeps their unknown fields.
- Long text is passed with `--summary-file` or `--description-file`, or as `-` to read it from stdin.
- Repeatable flags (`--email`, `--domain`, `--participant`, `--ref`) add values, on `create` and on `update`; each has a `--remove-<flag>` counterpart on `update`. No flag replaces a whole list.
- An empty value clears an optional field: `--agent ""`, `--due ""`.

### Compact views

Every compact view has `id`, the name or title, `live`, and `holder` on work records. Each type adds:

| Type | Fields |
|---|---|
| User | `kind`, `holder` |
| Organization | `domains`, `own` |
| Person | `emails` |
| Interaction | `occurred_at`, `channel`, `direction`, `subject`, `participants` |
| Task | `status`, `category`, `assignee`, `agent`, `due` |

## Discovery

`kravis schema [type]` returns a JSON Schema generated from the Go types. For each record type it gives:

- the catalog values that apply to the actor's holder, as enumerations;
- the category of every status and stage;
- the value used when a field is omitted;
- the default list order;
- the error codes that can occur.

It is the single source for agents, the skill, the future MCP server and the tests. The skill teaches agents to query it rather than copy it. `--help` is for people.

## Naming for agents

Agents arrive with other products' vocabulary. The CLI guides them back to Kravis's terms (see `GLOSSARY.md`):

- An unknown flag that is a common synonym gets a `hint` pointing to the Kravis flag: `--owner` points to `--assignee`, and `--delegate` points to `--agent`. Linear calls the agent working for a human its `delegate`, and most CRMs call the assignee `owner`.
- `role` appears only inside a party (`parties[].role`, `--role` on `deal party add`), never on its own. Future permissions will be called `permission` or `grant`.
- There is never a bare `source` field. In Salesforce and HubSpot it reads as the marketing lead source, so it always appears inside a source reference (`references[].source`).
- The holder works like Odoo's multi-company `company_id`: people and organizations are shared, and records of different holders cannot be linked. Help texts say so, because agents trained on Salesforce or Dynamics may read "organization" as the tenant.

## Commands in increment 1

| Command | Notes |
|---|---|
| `kravis init --timezone <zone> --organization <name> --user <key> [--user-name <name>]` | Creates `kravis.json`, `config.json` with the default catalogs, the first own organization and the first human, who is `created_by` of everything it creates and becomes the default actor in the local configuration. It validates everything before writing. On an initialized workspace it returns `existing` when the data matches, and otherwise fails with `WORKSPACE_EXISTS` and a hint. `kravis.json` is written last, so rerunning an interrupted `init` completes it |
| `kravis whoami [--set <user>]` | The resolved actor, its source and its holder; `--set` stores the default actor |
| `kravis schema [type]` | See Discovery |
| `kravis check` | Prints its findings (`code`, record, `message`) on stdout as `data` and exits 6 if there are any. They are data, not a failure of the command. It never writes |
| `kravis user list\|get\|create\|update\|delete` | `create <key> --human\|--agent --holder <org> [--handler <user>] [--person <id>]`; the ID is `us_<key>`. An agent needs a handler: a live human of the same holder, which cannot change in the MVP. Holder and kind never change. A user is deactivated with `update --live=false`, refused with `REFERENCED` while it is the handler of a live agent or the assignee or agent of a live task |
| `kravis organization list\|get\|create\|update\|delete` | `create --name <name>` (required). `--own` marks an own organization. The mark cannot be removed while the organization is a holder. `--domain` can be repeated and deduplicates |
| `kravis person list\|get\|create\|update\|delete` | `create [--name <name>] [--email <email>]…`: a name, an email or both. `--email` can be repeated and deduplicates; the email is also an alias of the person |
| `kravis person affiliation add\|remove <person> <organization> [--title <text>]` | At most one affiliation per pair. `add` on an existing pair updates the title |
| `kravis interaction create` | `--channel`, `--occurred-at`, `--subject`, `--participant` (repeatable), `--ref <source>:<kind>:<id>[@<scope>]` (repeatable), `--direction outbound\|inbound` (default `outbound`), `--initiated-by <user or participant>`, `--summary-file` |
| `kravis interaction list\|get\|update\|delete` | List filters: `--participant`, `--direction`, `--initiated-by [--with-agents]`, `--channel`, `--from`, `--to` |
| `kravis task create` | `--title`, `--assignee` (a human of the holder), `--agent`, `--due <date>`, `--purpose`, `--interaction <id>` (provenance: `from`), `--description-file`. A new task starts in `todo` |
| `kravis task status <task> <status>` | The only way to change the status |
| `kravis task list\|get\|update\|delete` | List filters: `--assignee`, `--agent`, `--category`, `--overdue`, `--due-before <date>`, `--all` |

Behavior to note:

- Adding a person with exactly one affiliation as a participant also adds that organization. With several affiliations nothing is added, and the response carries `MULTIPLE_AFFILIATIONS`.
- A task created with `--interaction` has a purpose (`follow-up` by default) and deduplicates by (interaction, purpose).
- Repeating an update or a transition with the same values returns `unchanged`.

## Increment 2 adds

- `deal create|list|get|update|delete`, `deal stage <deal> <stage> [--reason <lost reason>]`, `deal party add|remove <deal> <party> [--role <party role>]`, and the holder's party role on create.
- `project create|list|get|update|delete` and `project close|reopen`.
- `--project`, `--deal` and `--related` on tasks, plus task inheritance of the interaction's project or deal.
- `--deal` and `--project` on interactions.
- `note create|list|get|update|delete`.
- `--related-to` filters and the outreach and briefing recipes in the skill.

## Ready for later

- `--request-id`.
- `export` in NDJSON.
- A composite command for an interaction plus its follow-up.
- MCP.
- Text search.
- `--group-by`.
- `check --fix`.
- A count of results in other holders when a read is limited to the actor's holder. In the MVP the response only states the filter, in `filters.holder`.
