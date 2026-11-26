# CLI contract

The contract of the `kravis` command for agents, scripts and people. This document covers increments 1 and 2. All domain logic lives in the Go library; the CLI is a thin layer over it, and a future MCP server will be another thin layer over the same library and schemas.

## Shape

```text
kravis <type> <verb> [arguments] [flags]
kravis init | whoami | schema [type] | check
kravis search <text>
```

- The core verbs, uniform across types, are `list`, `get`, `create`, `update` and `delete`.
- A transition that has rules gets its own verb next to its type, and `update` cannot bypass it. These are `task status`, `deal stage`, `deal party add/remove` and `project close/reopen`.
- There are no composite operations, except `project close --cancel-tasks`, which is safe to repeat after a partial failure. Recording an interaction and its follow-up takes two calls, and each one is safe to repeat.

## Workspace

- Kravis works on the workspace given by `KRAVIS_WORKSPACE`, or else on the nearest folder holding `kravis.json`, from the current folder upwards. Outside any workspace, every command except `init` fails with `STORAGE_UNAVAILABLE` and a hint.
- `init` creates the workspace in the current folder, or in `KRAVIS_WORKSPACE` when it is set.

## Identity and holder

- Every write needs an actor. Kravis takes it from `--actor <user>`, then `KRAVIS_ACTOR`, then the local configuration: `$XDG_CONFIG_HOME/kravis/config.json`, by default `~/.config/kravis/config.json` on macOS and Linux (not Go's `os.UserConfigDir`, which points to `~/Library/Application Support` on macOS). It maps each workspace ID to its default actor, `{"workspaces": {"ws_…": {"actor": "us_…"}}}`. It belongs to a person on a machine, never to the workspace. A write without an actor fails with `MISSING_ACTOR` (exit code 2) and a hint naming both options, and so does `whoami` without `--set`, which has nothing to print. Other reads need no actor.
- The actor must exist and be live. In the MVP the actor is declared rather than authenticated: it serves attribution and auditing, and it is not a security control.
- A work record (deal, project, task, interaction and note) takes its holder from the actor, so there is no `--holder` flag on writes.
- An actor can only write the work records of its own holder: an update, transition or delete of another holder's record returns `NOT_FOUND`, also with `--all-holders`, which only widens reads.
- A read made with a declared actor is limited to the actor's holder, and the response states that filter: a `list` returns only that holder's work records, and a `get` of another holder's record returns `NOT_FOUND`. `--all-holders` removes the limit. A read without an actor is not limited. People, organizations and users are shared and never filtered.
- `kravis whoami` prints the resolved actor, where it came from (flag, environment or configuration) and its holder. `kravis whoami --set <user>` stores a live user as this workspace's default actor in the local configuration.

## Output

- Outside a TTY, every command prints exactly one JSON object, compact and on one line, including `--version` and usage errors such as an unknown command or flag (exit code 2, nothing on stdout). Only `--help` prints text for people. On a TTY it prints tables meant for people; `KRAVIS_OUTPUT=json` or `--output json` forces JSON there. `json` is the only value `--output` accepts; any other is `INVALID_VALUE` with `details.allowed: ["json"]`.
- An unknown command is `INVALID_VALUE`; `UNKNOWN_FIELD` is only for an unknown flag or field.
- `kravis --version` prints `{"data":{"version":"0.2.0"}}`, without the leading `v`, and `dev` only when no version is known. The version is the one injected at build time, else the module version from the build info (`go install …@vX.Y.Z`), else `dev`.
- Data goes to stdout and errors to stderr, so an error is never mixed with data in a pipe.

Success:

```json
{
  "data": { "...": "..." },
  "outcome": "created",
  "warnings": [{ "code": "SIMILAR_EXISTS", "message": "…", "details": { "id": "pe_elena-ruiz" } }]
}
```

- `data` is always present and never `null`: an empty list is `[]`.
- `warnings` appears only when there are warnings.
- `filters` appears only on reads. A list echoes in it every filter it applied, with the values after normalization (a person's email as its `pe_…` ID, a date as the instant it bounds): the holder limit as `holder`, the live-only default as `live: true`, and each flag the caller gave under its name with underscores (`due_before`, `initiated_by`, `with_agents`, `related_to`; `type` on `search`; `channel`, `party`, `stage`, `category` and the rest as they are). A flag that is off or absent is not listed, and neither are `holder` without a limit and `live` with `--all`, a `--stage` or a `--category`. A `get` states only the holder limit. For example, `kravis interaction list --channel email --participant e.ruiz@example.com` has `"filters": {"channel": "email", "holder": "org_acme-example", "live": true, "participant": "pe_elena-ruiz"}`.

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
  - users, organizations and people by name, compared as its slug (lowercase ASCII without accents, the same transformation that makes IDs), so the order is the same on every machine. A person without a name uses its smallest email (the emails are a sorted list), else its ID; a user without a name uses its ID. Both are compared the same way;
  - deals by name, compared as its slug; a deal without a name uses its ID;
  - projects by name, compared as its slug;
  - interactions by `occurred_at`, newest first;
  - tasks by `due`, soonest first, with tasks without a due date last;
  - notes by `created_at`, newest first;
  - `search` hits by `updated_at`, newest first (see [Search](#search)).

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
- `retryable` is `true` only for `VERSION_CONFLICT` and `STORAGE_UNAVAILABLE`, except that `STORAGE_UNAVAILABLE` is `false` when no workspace is found: waiting never fixes that.

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

### Error and warning codes

| Code | Kind | When |
|---|---|---|
| `INTERNAL` | error, 1 | An unexpected failure inside Kravis itself |
| `MISSING_ACTOR` | error, 2 | A write without an actor |
| `ACTOR_NOT_LIVE` | error, 2 | The actor does not exist or is not live |
| `UNKNOWN_FIELD` | error, 2 | Agent input with a field the schema does not define |
| `INVALID_VALUE` | error, 2 | A value outside its catalog or format, or an unknown command; `details.allowed` lists the valid values |
| `HOLDER_MISMATCH` | error, 2; check finding | A link to a record of another holder. As an error, `details.field` names the link; as a check finding, it is a link already on disk to a record of another holder |
| `NOT_FOUND` | error, 3 | The ID or alias does not exist |
| `VERSION_CONFLICT` | error, 4 | The record kept changing during 3 attempts |
| `DEDUP_CONFLICT` | error, 4 | The natural key exists with contradicting data, or identifying references are split across two interactions |
| `REFERENCED` | error, 4 | A delete of a record that others refer to (for an interaction that a task comes from, the hint names the steps: delete the tasks in `details.referrers`, then the interaction, then create the interaction and those tasks again), or closing a project that still has live tasks; `details.referrers` lists the records (the live tasks, for a close) |
| `WORKSPACE_EXISTS` | error, 2 | `init` with data that does not match the existing workspace, or that conflicts with the files an interrupted `init` left (another organization, user or timezone) |
| `STORAGE_UNAVAILABLE` | error, 5 | The workspace cannot be read or written |
| `WORKSPACE_FILE_MISSING` | check finding | `kravis.json` or `config.json` does not exist |
| `WORKSPACE_FILE_INVALID` | check finding | `kravis.json` or `config.json` exists but cannot be read as the file it should be |
| `ORPHAN_SIDECAR` | check finding | A `.md` sidecar has no `.json` record next to it |
| `UNREADABLE_RECORD` | check finding | A record file exists but cannot be read |
| `MALFORMED_RECORD` | check finding | A record file is not valid JSON for a record, or has no `id` |
| `INVALID_RECORD` | check finding | A record breaks its type's rules: an ID of the wrong form or that does not match its file name, a field of the wrong type, no `live` flag, a `version` below 1, no `created_at` or no `created_by` (an update of such a record is refused with `INVALID_VALUE` and a hint to repair it by hand; Kravis never invents them), a person with neither name nor email, a holder left without a live human by two retirements that raced (one whose users include no live human, or any holder when the workspace has no live human left), an invalid or repeated email or domain, or a provider domain on an organization. Also: a deal whose category or `live` disagrees with its stage, with a lost reason outside the lost category or none inside it, or whose holder is one of its parties; a task with both a project and a deal; a live task in a project that is not live; a note without subjects or without its text sidecar |
| `KEY_WITHOUT_VALUE` | check finding | A deduplication key points to a record that does not exist or no longer has the value |
| `VALUE_WITHOUT_KEY` | check finding | A record has an email or domain whose deduplication key is missing or points to another record |
| `BROKEN_REFERENCE` | check finding | A user, an affiliation or a deal refers to a record that does not exist: a holder, a handler, a linked person or an organization; a deal's party |
| `DANGLING_REFERENCE` | check finding | A work record refers to a record that does not exist: an interaction's holder, participant, initiator, deal or project; a task's project, deal or related record; a project's deal or related record; a note's subject |
| `VALUE_NOT_IN_CATALOG` | check finding | A catalog-valued field holds a value that the catalog for its holder no longer has, such as an interaction's channel, a deal's stage, party role, holder role or lost reason, or a task's status; the next update of the record must replace it. A task's purpose is the exception: it never changes, so a purpose in use is grandfathered. Removing it from the catalog only stops new tasks from taking it; the tasks that have it keep it, `update` does not require replacing it, and `check` does not report it |
| `SIMILAR_EXISTS` | warning | A slug got a `-2` suffix; `details.id` names the existing record |
| `GENERIC_ADDRESS` | warning | A person was created from `info@`, `noreply@` or a similar address |
| `MULTIPLE_AFFILIATIONS` | warning | A participant person has several affiliations, so none was added |
| `UNKNOWN_SOURCE` | warning | A source reference to a source that is not in the catalog |

A check finding is data on `kravis check`, with exit code 6, not an error.

### Input

- Agent input is strict: unknown flags and fields are refused (`UNKNOWN_FIELD`). Records read from disk are tolerant instead: Kravis keeps their unknown fields.
- Long text is passed with `--summary-file`, `--description-file` or `--text-file`, or as `-` to read it from stdin. Text from stdin drops one trailing newline (the one a pipe adds); text from a file is kept as is.
- Repeatable flags (`--email`, `--domain`, `--participant`, `--ref`, `--role`, `--related`, `--subject`, `--deal`, `--project` on interactions) add values, on `create` and on `update`; each has a `--remove-<flag>` counterpart on `update`, except `--role`, which changes through `deal party`. No flag replaces a whole list.
- `deal create` takes one `--party`, the deal's first party; repeating it is `INVALID_VALUE` with `details.field: "party"` and a hint to add the others with `deal party add`. `--role` repeats for that party. The other parties come with `deal party add` (see [Deals](#deals)): `kravis deal create --party pe_ben-carter --holder-role advisor`, then `kravis deal party add dl_ben-carter pe_mira-vale --role client`.
- An empty value clears an optional field: `--agent ""`, `--due ""`.

### Compact views

Every compact view has `id`, the name or title, `live`, and `holder` on work records. Each type adds:

| Type | Fields |
|---|---|
| User | `kind`, `holder` |
| Organization | `domains`, `own` |
| Person | `emails` |
| Interaction | `occurred_at`, `channel`, `direction`, `subject`, `participants` |
| Task | `status`, `category`, `assignee`, `agent`, `due`, `project`, `deal` |
| Deal | `name`, `stage`, `category`, `holder_role`, `lost_reason` |
| Project | `deal` |
| Note | `subjects`, `excerpt`, `created_at` |

A note has no title, so its view has no name: `excerpt` is the first line of its text, cut at 80 characters. A deal's name is optional and omitted when empty. `--full` adds what the compact view leaves out (`--fields` picks some of it), and plain `get` stays compact: a deal's `pipeline`, `parties` and description; a project's `related` and description; a task's `related` and description; an interaction's `deals` and `projects`; a note's full `text`.

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
- `role` appears only inside a party (`parties[].roles`, `--role` on `deal party add`), never on its own. Future permissions will be called `permission` or `grant`.
- There is never a bare `source` field. In Salesforce and HubSpot it reads as the marketing lead source, so it always appears inside a source reference (`references[].source`).
- The holder works like Odoo's multi-company `company_id`: people and organizations are shared, and records of different holders cannot be linked. Help texts say so, because agents trained on Salesforce or Dynamics may read "organization" as the tenant.

## Commands

| Command | Notes |
|---|---|
| `kravis init --timezone <zone> --organization <name> --user <key> [--user-name <name>] [--user-email <email>]` | Creates `kravis.json`, `config.json` with the default catalogs, the first own organization and the first human, who is `created_by` of everything it creates and becomes the default actor in the local configuration. With `--user-email`, the first user is linked to the person with that email: if no person has it, `init` creates one, named `--user-name` or, without it, only with the email; if one exists, it is linked unchanged, even with another name. It validates everything before writing. On an initialized workspace it returns `existing` when the data matches, and otherwise fails with `WORKSPACE_EXISTS` and a hint (another `--user-email` is data that does not match, like any other). `kravis.json` is written last, so rerunning an interrupted `init` with the same data completes it; with data that conflicts with the files it left (another organization, user or timezone) it fails with `WORKSPACE_EXISTS` |
| `kravis whoami [--set <user>]` | The resolved actor, its source and its holder; without `--set` and without an actor it fails with `MISSING_ACTOR`. `--set` stores the default actor and needs no actor |
| `kravis schema [type]` | See Discovery |
| `kravis search <text> [--type <type>]… [--all] [--limit <n>] [--cursor <c>]` | Finds records by their own text, without knowing their type or ID. See [Search](#search) |
| `kravis check` | Prints its findings (`code`, record, `message`) on stdout as `data` and exits 6 if there are any. They are data, not a failure of the command. It never writes |
| `kravis user list\|get\|create\|update\|delete` | `create <key> --human\|--agent --holder <org> [--handler <user>] [--person <id>]`; the ID is `us_<key>`. An agent needs a handler: a live human of the same holder, which cannot change in the MVP. Holder and kind never change. A user is deactivated with `update --live=false`, refused with `REFERENCED` while it is the handler of a live agent or the assignee or agent of a live task. Deactivating or deleting the last live human of a holder is refused before any change with `INVALID_VALUE`, `details.holder` and a hint to create or reactivate another human first, so someone can still act for the holder |
| `kravis organization list\|get\|create\|update\|delete` | `create --name <name>` (required). `--own` marks an own organization. The mark cannot be removed while the organization is a holder. `--domain` can be repeated and deduplicates |
| `kravis person list\|get\|create\|update\|delete` | `create [--name <name>] [--email <email>]…`: a name, an email or both. `--email` can be repeated and deduplicates; the email is also an alias of the person |
| `kravis person affiliation add\|remove <person> <organization> [--title <text>]` | At most one affiliation per pair. `add` on an existing pair updates the title |
| `kravis interaction create` | `--deal` and `--project` (repeatable, of the holder), `--channel`, `--occurred-at`, `--subject`, `--participant` (repeatable), `--ref <source>:<kind>:<id>[@<scope>]` (repeatable), `--direction outbound\|inbound` (default `outbound`), `--initiated-by <user or participant>`, `--summary-file` |
| `kravis interaction list\|get\|update\|delete` | List filters: `--participant`, `--direction`, `--initiated-by [--with-agents]`, `--channel`, `--from`, `--to`, `--deal`, `--project`. A person's outreach in a month is `--direction outbound --initiated-by <user> --from <first day> --to <last day>` (INTERACTIONS-AC-02). `update` adds and removes deals and projects with `--deal`, `--project`, `--remove-deal` and `--remove-project` |
| `kravis task create` | `--title`, `--assignee` (a human of the holder), `--agent`, `--due <date>`, `--purpose`, `--interaction <id>` (provenance: `from`), `--project <project>` or `--deal <deal>` (its parent), `--related <id>` (repeatable), `--description-file`. A new task starts in `todo` |
| `kravis task status <task> <status>` | The only way to change the status |
| `kravis task list\|get\|update\|delete` | List filters: `--assignee`, `--agent`, `--category`, `--overdue`, `--due-before <date>`, `--project`, `--deal`, `--related-to <id>`, `--all`. `update` adds and removes related records with `--related` and `--remove-related`, and cannot change or clear the parent |

Behavior to note:

- Adding a person with exactly one affiliation as a participant also adds that organization. With several affiliations nothing is added, and the response carries `MULTIPLE_AFFILIATIONS`.
- A task created with `--interaction` has a purpose (`follow-up` by default) and deduplicates by (interaction, purpose).
- Repeating an update or a transition with the same values returns `unchanged`.

### Deals

| Command | Notes |
|---|---|
| `kravis deal create --party <person or organization> [--role <party role>]… --holder-role <party role> [--name <name>] [--stage <stage>] [--description-file]` | `--party` is required and is the deal's first party, with its roles (`--role`, repeatable, optional). `--holder-role` is the holder's party role and has no default. `--name` is optional. The stage defaults to the first stage of the pipeline, `prospect`; `--stage` on create refuses a stage of the lost category with `INVALID_VALUE` and a hint to use `deal stage`, which takes the reason. The ID is `dl_<slug>`, the party's ID without its prefix, then `dl_<slug>-2`, `-3` for the next deals with the same party, without a `SIMILAR_EXISTS` warning; it never changes, even if that party is later removed. A deal has no natural key, so repeating a create makes another deal. It is stored with `pipeline: "default"`. There is no `--pipeline` flag yet |
| `kravis deal list\|get\|update\|delete` | List filters: `--party <id>`, `--stage`, `--category`, `--all`. `update` changes `--name`, `--holder-role` and the description. `--stage` is refused as an unknown flag with a hint pointing to `deal stage`, and `live` cannot be set: it follows the stage's category (ongoing is live) |
| `kravis deal stage <deal> <stage> [--reason <lost reason>]` | The only way to change the stage, and it may move to any stage of the pipeline. The same stage with the same reason (or none) is `unchanged`. A stage outside the pipeline is `INVALID_VALUE` with `details.allowed` listing the stages. A stage of the lost category needs `--reason`, else `INVALID_VALUE` with `details.field: "reason"` and the lost reasons in `details.allowed`; `--reason` with any other stage is `INVALID_VALUE` too. A move to a stage outside the lost category, ongoing or won, clears the reason; lost to lost with another reason replaces it |
| `kravis deal party add <deal> <party> [--role <party role>]…` | A party is a person (ID or email) or an organization, at most once per deal. On an existing party, `add` adds the missing roles and returns `unchanged` when all are there. The roles are optional. The deal's holder as a party is `INVALID_VALUE` with `details.field: "party"` |
| `kravis deal party remove <deal> <party> [--role <party role>]` | Without `--role` it removes the party; with it, only that role, and the party stays even with none left. A party or a role that is not on the deal is `NOT_FOUND` |

### Projects

| Command | Notes |
|---|---|
| `kravis project create --name <name> [--deal <deal>] [--related <id>]… [--description-file]` | The ID is `pr_<slug>` of the name, with `-2` and `SIMILAR_EXISTS` on a collision. A project is tied to at most one deal and a deal may have many projects. A project is created live |
| `kravis project list\|get\|update\|delete` | List filters: `--deal`, `--all`. `update` changes the name, `--deal` (at any time: a task in the project takes its deal from the project, so none can disagree), `--related` and `--remove-related`, and the description. It cannot set `live` |
| `kravis project close <project> [--cancel-tasks]` | Closing a project already closed is `unchanged`. With live tasks it fails with `REFERENCED` and the live tasks in `details.referrers`. `--cancel-tasks` first moves every live task of the project to the first status of the `canceled` category in the catalog, then closes it; repeating it after a partial failure finishes the rest. `data` lists the tasks it canceled in `canceled_tasks` |
| `kravis project reopen <project>` | Reopening a live project is `unchanged` |

A task created in a closed project, or moved by `task status` to a live status while its project is closed, is refused with `INVALID_VALUE` (`details.field: "project"`) and a hint to reopen the project.

### Notes

| Command | Notes |
|---|---|
| `kravis note create --subject <id>… --text-file <file>` | `--subject` is repeatable, at least one, each a person (ID or email), organization, deal or project; deals and projects must be of the holder (`HOLDER_MISMATCH` otherwise). The text is required and lives in the `.md` sidecar. The ID is a TypeID. A note is always live: a note that is no longer useful is deleted |
| `kravis note list\|get\|update\|delete` | List filter: `--related-to <id>`, a subject. `update` changes the text and adds or removes subjects with `--subject` and `--remove-subject`, never below one |

### Parents, related records and refusals

- A task has at most one parent. `--project` together with `--deal` is `INVALID_VALUE`. A task in a project stores only the project and takes the deal from it; a task stores a deal only when the deal is its direct parent. The parent is fixed at creation: to fix a wrong one, delete the task and create it again.
- A task created with `--interaction` and no parent takes the interaction's project or deal when it has exactly one between both; with several it fails with `INVALID_VALUE`, `details.field: "project"` and the candidates in `details.allowed`; with none it has no parent. An explicit parent wins.
- `--related` takes people, organizations, deals and projects; a deal or project of another holder is `HOLDER_MISMATCH` and any other type `INVALID_VALUE`.
- `--related-to <id>` on `task list` matches a task related to the record, or whose parent is that deal or project, or, for a deal, whose project is tied to it, or whose source interaction (`from`) has the record as a participant. `--deal` on `task list` matches the same way: tasks of the deal and tasks of its projects. `--deal` on `interaction list` likewise matches interactions linked to the deal and those linked only to a project tied to it. On `note list` it matches a subject. Other lists have only their specific filters.
- `delete` is `REFERENCED`, with the referrers, while any record refers to the target. A deal is referred to by its projects, tasks, interactions, notes and the related lists of tasks and projects; a project, by its tasks, interactions, notes and related lists; a person or an organization, also by deals (as a party), notes (as a subject) and related lists. A note is referred to by nothing.
- The skill lists the outreach recipe (the interaction filters above) and the briefing recipe (a person's or organization's interactions, notes with `--related-to`, deals with `--party`, and what is due today); their text is in the skill, not here.

## Search

`kravis search <text>` is the one global text search. Lists have no `--match`; type-specific filters stay on each `list`.

- **Match.** The text is split on whitespace into words. A record matches when every word appears in at least one of its searched fields, in any order, each word as a substring anywhere (no wildcards). Case and accents are ignored: text and fields are folded as slugs are (lowercase, accents removed), and other characters are kept as they are.
- **Searched fields**, only the record's own text: a person's name and emails; an organization's name and domains; an interaction's subject and summary; a task's title and description; a deal's and a project's name and description; a note's text. IDs and links are not searched. To get what is linked to a hit, `search`, then the filters of each list (`--related-to`, `--participant`, `--party`, `--deal`, `--project`, as that list has them).
- **Scope.** The records of the actor's holder plus the shared directory (people and organizations), stated in `filters.holder`; `--all-holders` removes the limit, and a search without an actor is not limited, with no `filters.holder`, exactly as in a `list`. Users are not searched.
- **Flags.**
  - `--type <type>` is repeatable and narrows the search to those types: `person`, `organization`, `interaction`, `task`, `deal`, `project`, `note`. An unknown type, `user` included, is `INVALID_VALUE` with `details.field: "type"` and the searchable types in `details.allowed`.
  - `--all` includes records that are not live. By default only live records are searched and `filters.live` is `true`; with `--all` it is not listed. Notes are always live.
  - `--limit` and `--cursor` work as in lists.
- **Response.** The list shape: `data`, `filters`, `total` and `next_cursor`. `filters` holds `holder`, `live`, `type` when given and `text`. Each hit in `data` has:
  - `type`;
  - the compact view of its type;
  - `updated_at`;
  - `matches`: one `{"field": "<field>", "excerpt": "<text>"}` for each field that matched. A short field (name, email, domain, title, subject) gives its whole value; when the field holds several values (emails, domains), the first one, in stored order, that contains a word of the text. A long text (summary, description, note text) gives a window of 80 characters around the first match, with `…` where the text is cut.
- **Order.** By `updated_at`, newest first, with the ID breaking ties.
- **Errors.** Text with no words (empty or only spaces) is `INVALID_VALUE` with `details.field: "text"` and a hint to give at least one word. A bad `--limit` or `--cursor` is `INVALID_VALUE` as in lists. A search that finds nothing is `data: []`, not an error.

For example, `kravis search "ruiz acme"` has `"filters": {"holder": "org_acme-example", "live": true, "text": "ruiz acme"}` and a hit such as `{"type": "person", "id": "pe_elena-ruiz", "name": "Elena Ruiz", "emails": ["e.ruiz@acme.example.com"], "live": true, "updated_at": "2026-11-20T10:00:00Z", "matches": [{"field": "name", "excerpt": "Elena Ruiz"}, {"field": "emails", "excerpt": "e.ruiz@acme.example.com"}]}`.

## Ready for later

- `--request-id`.
- `export` in NDJSON.
- A composite command for an interaction plus its follow-up.
- MCP.
- `--group-by`.
- `check --fix`.
- A count of results in other holders when a read is limited to the actor's holder. In the MVP the response only states the filter, in `filters.holder`.
