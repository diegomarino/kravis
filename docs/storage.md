# Storage

How Kravis keeps a workspace on disk. This document covers increments 1 to 3. Behavior lives in the ddduck model (`docs/ddd/`); this document covers how that behavior is stored.

## Principles

- **Files are the only source of truth.** There is no cache and no index: reads scan the files. A future index must be disposable and rebuildable from the files.
- **One record, one JSON file.** The file is readable in a diff and can be filtered with `jq`. Long text goes in a Markdown sidecar next to it.
- **Object-store semantics.** Every storage operation goes through five calls, so the same contract can later run on an S3-compatible bucket such as Tigris:
  - `get(path)` returns the content and a version token, which names one write, not the content: writing the same bytes again gives a new token;
  - `create(path, bytes)` writes only if the path does not exist;
  - `replace(path, bytes, token)` writes only if the token still matches;
  - `delete(path, token)` removes the object only if the token still matches;
  - `list(prefix)` lists paths under a prefix.
  Nothing outside the connector assumes POSIX: no hard links, no `rename` and no `fsync` in the domain code.
- **No invariant needs several files to be written atomically.** Rules that span records are checked when writing, and any race left over is reported by `kravis check`.
- **Git, when present, is the history.** Kravis never commits and never depends on git.

## Layout

```text
<workspace>/
  kravis.json                       workspace ID and schema version
  config.json                       timezone and catalogs
  records/
    users/us_<key>.json
    organizations/org_<slug>.json
    people/pe_<slug>.json
    interactions/<yyyy-mm>/it_<yyyy-mm-dd>-<slug>.json   (+ .md)
    tasks/<yyyy-mm>/tk_<typeid>.json                     (+ .md)
    deals/dl_<slug>.json                                 (+ .md)
    projects/pr_<slug>.json                              (+ .md)
    notes/<yyyy-mm>/nt_<typeid>.json + .md
  keys/<kind>/<h0h1>/<hash>.json    deduplication keys
```

- A record's path is computed from its ID alone, with no lookup.
- Nothing is ever moved: closing or reopening a record only changes fields.
- Interactions are grouped by the month in which they occurred, which never changes. Tasks and notes are grouped by the month in which they were created, taken from the TypeID timestamp in UTC, so the folder never depends on the workspace timezone. A month folder gives a cheap scope for recent records.
- Civil dates used in IDs and in interaction folders are computed in the workspace timezone (`config.json`, an IANA zone such as `Europe/Madrid`), never in the machine's local zone. Instants are stored in UTC (RFC 3339). Changing the timezone only affects new records.

## Identifiers

| Type | Form | Example |
|---|---|---|
| User | `us_<key>`, with a free key; recommended `<name>-<holder>` when there are several holders | `us_diego-petalo`, `us_correo` |
| Organization | `org_<slug>` from the name | `org_petalo` |
| Person | `pe_<slug>`: from the display name if known, otherwise the local part of the email plus the first label of its domain | `pe_elena-ruiz`, `pe_e-ruiz-gruposierra` |
| Interaction | `it_<yyyy-mm-dd>-<slug>`: the date it occurred plus the subject, ASCII and truncated to about 40 characters | `it_2026-11-23-documentacion-data-room` |
| Task | `tk_<typeid>` (prefix + UUIDv7 in base32) | `tk_01jdx…` |
| Deal | `dl_<slug>`: the slug of the first party's ID, without its prefix; the next deal with that party gets `-2`, `-3`… with no warning | `dl_grupo-sierra`, `dl_grupo-sierra-2` |
| Project | `pr_<slug>` from the name | `pr_data-room` |
| Note | `nt_<typeid>` | `nt_01jdx…` |

- Slugs are lowercase ASCII with hyphens. On a collision the CLI adds `-2`, `-3`… and returns the `SIMILAR_EXISTS` warning with the existing ID, except for deals, where a second deal with the same party is normal.
- Kravis generates every ID, except a user's key, which is given at creation (`kravis user create <key>`, `init --user <key>`). An ID never changes afterwards: the name can be edited, but the ID cannot.
- Each type has exactly one ID form.
- A person can also be addressed by any of their emails wherever a person ID is accepted.
- Emails are compared trimmed and lowercased as a whole; domains lowercased, without a leading `www.`. Nothing else is rewritten (no removal of dots or `+tags`).
- Generic addresses and domains are a list in code: local parts such as `info`, `hello`, `contact`, `noreply`, `no-reply`, `support`, `sales`, `admin`, `team`, `office`; email providers such as `gmail.com`, `googlemail.com`, `outlook.com`, `hotmail.com`, `live.com`, `yahoo.com`, `icloud.com`, `me.com`, `proton.me`, `protonmail.com`, `gmx.com`. A generic address gives `GENERIC_ADDRESS`; a provider domain is never an organization's domain.
- Catalog values (statuses, party roles, purposes, lost reasons, channels, stages) are slugs: they are configuration, not records.

## Record format

```json
{
  "id": "tk_01jdx7v3k2f9q8w4e6r5t2y1u0",
  "holder": "org_petalo",
  "title": "Send the teaser to Elena",
  "status": "todo",
  "category": "open",
  "live": true,
  "assignee": "us_diego-petalo",
  "agent": "us_correo-petalo",
  "from": "it_2026-11-23-documentacion-data-room",
  "purpose": "follow-up",
  "due": "2026-11-28",
  "version": 3,
  "created_at": "2026-11-23T17:02:11Z",
  "created_by": "us_correo-petalo",
  "updated_at": "2026-11-24T09:15:40Z",
  "updated_by": "us_diego-petalo"
}
```

- Two-space indentation, one field per line, UTF-8 without escaping non-ASCII characters, and a trailing newline.
- Fields follow a fixed order per type: `id`, then the domain fields in schema order, then the metadata (`version`, `created_at`, `created_by`, `updated_at`, `updated_by`).
- Lists of references (participants, parties, subjects, related records) are sorted by ID.
- Empty fields are omitted: a missing field means empty, so adding an optional field to the schema leaves existing files untouched.
- Every record has `live`. Records with a status or stage also store its `category`. Kravis computes both on every write, and `kravis check` reports when they disagree with the catalog.
- Every write increments `version` and sets `updated_at` and `updated_by` from the actor.
- Unknown fields found on disk are kept when the record is rewritten. Agent input is decoded strictly instead (see `docs/cli.md`).

### Deals, projects and notes

Required fields are marked; every other field is omitted when empty.

| Type | Fields in order |
|---|---|
| Deal | `id`, `holder`, `name`, `pipeline` (required, `default`), `stage` (required), `category` (required), `holder_role` (required), `lost_reason`, `parties`, `live`, then the metadata |
| Project | `id`, `holder`, `name` (required), `deal`, `related`, `live`, then the metadata |
| Note | `id`, `holder`, `subjects` (at least one), `live` (always `true`), then the metadata |

- A deal's `parties` is a list of `{"party": "<person or organization ID>", "roles": ["client"]}`, sorted by party. A deal's `live` is true while its category is `ongoing`. `lost_reason` is present only in the `lost` category.
- A project's `live` is false once it is closed.
- A task adds `project` or `deal` (its parent, never both) and `related`. A task in a project stores no deal: it takes it from the project.
- An interaction adds `deals` and `projects`.
- `related` and `subjects` hold person, organization, deal or project IDs, sorted.

### Sidecars

- Long text lives in `<id>.md` next to the JSON file, and the file exists only when there is text. The text is an interaction's summary, a task's, deal's or project's description, or the content of a note (notes always have one).
- On write, Kravis writes the `.md` first and the JSON second. The JSON carries the version and confirms the write.
- `check` reports sidecars that have no JSON next to them, and a note without its sidecar. Other texts are optional, and nothing in the JSON says a sidecar should exist, so a missing one reads as no text. It does not try to tell who edited a file: it validates every record against the rules (format, catalogs, category and `live`, references that exist and share the holder, deduplication keys), so a correct edit made by hand is valid and a wrong one is reported.

## Configuration

`kravis.json`:

```json
{ "workspace": "ws_<typeid>", "schema_version": 1 }
```

`config.json`:

```json
{
  "timezone": "Europe/Madrid",
  "catalogs": {
    "task_statuses": [
      { "name": "todo", "category": "open" },
      { "name": "in-progress", "category": "open" },
      { "name": "waiting", "category": "waiting" },
      { "name": "done", "category": "done" },
      { "name": "canceled", "category": "canceled" }
    ],
    "pipelines": {
      "default": {
        "stages": [
          { "name": "prospect", "category": "ongoing" },
          { "name": "engaged", "category": "ongoing" },
          { "name": "closing", "category": "ongoing" },
          { "name": "won", "category": "won" },
          { "name": "lost", "category": "lost" }
        ]
      }
    },
    "purposes": ["follow-up", "ping", "expand", "intro", "prepare"],
    "channels": ["email", "meeting", "call", "message"],
    "party_roles": ["client", "counterparty", "advisor", "lender", "investor", "buyer", "seller"],
    "lost_reasons": ["price", "timing", "competitor", "no-decision", "ghosting", "fit", "other"]
  }
}
```

- `init` writes these defaults, and each workspace may edit them. The MVP has the single pipeline `default`, and every deal stores its `pipeline`, so more pipelines can be added later without a migration.
- Catalog lookups always go through "the configuration for holder H". In the MVP that call returns the workspace configuration, so per-holder configuration can be added later without touching callers.
- The timezone, the workspace ID and the schema version are always workspace-wide.
- A catalog value removed while records still use it stays on those records. `check` reports it, per holder, and the next edit of the record must replace it. A task's purpose is the exception: it never changes, so a purpose in use is grandfathered and `check` does not report it.

## Writes and concurrency

- **Create** uses `create`, which writes only if the path does not exist.
- **Update** reads the record, applies the change, and writes with `replace`, conditional on the version token it read. If the record changed in between, Kravis rereads and reapplies the change, up to 3 attempts. If it still conflicts, it fails with `VERSION_CONFLICT` (exit code 4).
- The local filesystem connector writes to a temporary file, checks that the version is unchanged right before renaming it into place, and takes no locks. The residual window between the check and the rename lasts under a millisecond and is accepted: a change lost in it cannot be detected afterwards. Hardening it (a lock per record locally, `If-Match` / `If-None-Match` on Tigris) is a connector change that leaves the contract as it is.
- **Delete** checks that no record refers to the target, reads the tokens of its sidecar and of the keys that point to it, then removes the JSON file with `delete`, conditional on the version token it read, and then the sidecar and those keys, each conditional on the token read before. A target that changed in between fails with `VERSION_CONFLICT`. If something refers to the target, it fails and lists the referring records.
- Once the JSON file is gone, a create may reuse its path before the cleanup ends. What that create writes has new tokens, so the cleanup leaves it: a sidecar or key whose token changed is not the deleted record's.

### Deduplication keys

A natural key is stored as a file that points to the record holding it:

```json
{ "key": "interaction|org_petalo|email:message:<message-id>", "record": "it_2026-11-23-documentacion-data-room" }
```

- The path is `keys/<kind>/<first two hex characters>/<sha256 of key>.json`.
- Creating a record with a natural key works in three steps:
  1. Kravis allocates the ID.
  2. It creates the key file with `create`.
  3. It creates the record with that ID.
- If the key file already exists, Kravis reads the record it points to and returns `existing`. If that record is missing because a previous attempt crashed between steps 2 and 3, Kravis creates it now with the ID stored in the key, so a retry completes the work instead of duplicating it. It rewrites that key file first, so the key's token is new and the cleanup of a delete of the record's earlier version leaves it.
- If the data sent contradicts the existing record, the result is a deduplication conflict (exit code 4).
- An update that adds an identifying value (an email, a domain, an identifying source reference) creates its key first; if the key already points to another record, the update fails with `DEDUP_CONFLICT` and writes nothing. Then it writes the record, and then it deletes the keys of the values it removed. An interrupted update can leave a key without its value or a value without its key; `check` reports both.
- A task's origin (interaction and purpose) never changes after creation, so its `task-origin` key never moves.

| Kind | Key | Increment |
|---|---|---|
| `person-email` | each email of a person | 1 |
| `organization-domain` | each domain of an organization (generic email domains excluded) | 1 |
| `interaction-ref` | (holder, canonical identifying source reference) | 1 |
| `task-origin` | (interaction, purpose) | 1 |
| `requests` | reserved for `--request-id` | — |

Increment 2 adds no key: deals, projects and notes have no natural key.

### Sources

The catalog of sources lives in code, because each source has its own behavior: the shape of its references, how they are made canonical, and which of them identify an interaction.

A reference is written `<source>:<kind>:<id>[@<scope>]`. The source and the kind run up to the second `:`. Only sources whose IDs are unique within an account take a scope, which starts at the first `@` after the ID, because those IDs contain no `@` and accounts do.

| Source:kind | `id` | Scope | Identifies an interaction |
|---|---|---|---|
| `email:message` | The Message-ID header without `< >`, as given by Gmail, Outlook (`internetMessageId`), Apple Mail or IMAP | — | Yes |
| `ical:event` | The iCalendar UID (Google `iCalUID`, Outlook `iCalUId`); for one occurrence of a recurring event, `<uid>/<start in UTC, RFC 3339>` | — | Yes |
| `granola:meeting` | The meeting ID | — | Yes |
| `slack:message` | `<channel ID>/<ts>` | — | Yes |
| `telegram:message` | `<chat ID>/<message ID>` | — | Yes |
| `gmail:message`, `gmail:thread`, `outlook:message` | The provider's ID | The account, required | No: a link back to the original |
| `drive:file`, `onedrive:file` | The file ID | — | No |
| `web:url` | The URL, with scheme and host lowercased and no fragment | — | No |

- Canonical form: the source, the kind and the scope are lowercased; the ID is kept as given, except for `web:url`. A reference that misses a required scope, or has a scope its source does not take, is refused with `INVALID_VALUE`.
- The same email seen through two tools (a Gmail connector and Apple Mail, or two users of the same holder) has one Message-ID, so it is one interaction. An agent whose tool cannot give the Message-ID records only the provider's reference, which does not deduplicate.
- Adding a source is one row in this catalog and changes no record.
- An unknown source is accepted with the `UNKNOWN_SOURCE` warning. It identifies an interaction only by exact equality of the reference, and its shape is not validated.
- Kravis stores references and summaries, never a copy of the original content.

## Recovery

Stop other agents from writing to the workspace first. Both procedures were run in a scratch workspace.

### A stale deduplication key (`KEY_WITHOUT_VALUE`)

An interrupted update can leave a key file that points to a record that no longer has the value (see Deduplication keys). Until it is released, creating the rightful owner of that value fails with `DEDUP_CONFLICT`.

1. `kravis check` names the key file in `record` and the value in `message`, for example `person-email "b@example.com" points to pe_ana-test, which does not have it`.
2. Check ownership. Open the key file: `record` is the record the interrupted update was changing. `kravis person get <that record> --fields emails` must not list the value, and no other record may hold it (`grep -rlF '"<value>"' records`, which matches the whole JSON string, not a mention in a note or summary). If some record does hold it, the key is not stale: do not remove it, and `check` reports `VALUE_WITHOUT_KEY` or a conflict on that record instead.
3. Decide what the interrupted update meant. To finish it, repeat it: `kravis person update <record> --email <value>` (an organization's domain works the same). To abandon it, release the key: move the key file out of `keys/` (`mv <key file> <backup folder>`), never edit it by hand.
4. `kravis check` returns `[]` with exit code 0, and creating the rightful record works.

### A missing `config.json` in an initialized workspace

`kravis.json` is there but `config.json` is not: every command fails with `STORAGE_UNAVAILABLE`, `kravis check` reports `WORKSPACE_FILE_MISSING` for `config.json`, and `kravis init` refuses with `WORKSPACE_EXISTS`. `init` cannot rebuild the file, and the timezone and catalogs are not recorded anywhere else, so do not run it again.

1. Restore the file you had: from a backup, or from git if the workspace is a repository (`git checkout -- config.json`, or `git show <commit>:config.json > config.json`).
2. `kravis check` returns `[]` with exit code 0, and ordinary reads work again.

Without a copy, write `config.json` by hand in the format of Configuration above, with the original timezone: civil dates in IDs depend on it.

## Ready for later

- **Tigris:** an S3 connector implementing the same five calls with conditional writes and deletes. tigrisfs does not preserve conditional writes, and each machine may see a cached version for seconds, so a workspace on tigrisfs supports a single writer; several writing machines need this connector. A bucket with writers in several regions must be Single-region or Multi-region.
- **History on Tigris:** a change journal of Kravis's own. It would not change the record format.
- **Index:** a disposable index, to be built when listing open tasks across all history becomes slow.
