---
name: kravis
description: Use Kravis, a CRM for agents, to record interactions with people and organizations, follow them up with tasks, and keep the history in one workspace of JSON files. Use it when asked to log an email, meeting or call, to remember who was involved, to create or move a follow-up, to run a deal or a project, to write a note, to see what is overdue or due today, to prepare a briefing on a person or organization, or to count a person's outreach.
---

# Kravis

Kravis is a command, `kravis`, over a workspace: a folder with one JSON file per record. Force JSON with `--output json` (or `KRAVIS_OUTPUT=json`) on every call: on a TTY, including a PTY-backed terminal, the default is a table. Then a success is one JSON object on stdout, and an error is one on stderr with a `code` and `details`, and usually a `hint`: it may be absent. `--help` is always text. Read the `hint` and `details.allowed` before you retry. The full contract is in [docs/cli.md](../../docs/cli.md#output).

## Find the workspace and the actor

- The workspace is `KRAVIS_WORKSPACE`, or the nearest folder with `kravis.json` above the current one. Outside any workspace every command fails with `STORAGE_UNAVAILABLE`. In a new folder run `kravis init --timezone <zone> --organization <name> --user <key>`.
- A write needs an actor, a live user: `--actor <user>`, then `KRAVIS_ACTOR`, then the default stored on this machine. `kravis whoami` shows who you are and for which holder. `kravis whoami --set <user>` stores the default.
- `init` creates the first human. Add another human with `kravis user create <key> --human --holder <organization>` and an agent with `kravis user create <key> --agent --holder <organization> --handler <human>`: an agent needs a live human of the same holder as its handler. The ID is `us_<key>`.
- A work record (deal, project, task, interaction, note) belongs to the holder of the actor. A write is always limited to your holder's. A read is limited to it only when an actor is declared (`--actor`, `KRAVIS_ACTOR` or the stored default): with none, `list` returns every holder's records. Run `kravis whoami` before any holder-specific read and check `filters.holder` in the response: when it is absent, nothing was limited. To read one holder's records, pass its user: `kravis task list --actor <user>`. `--all-holders` lifts the limit. People, organizations and users are shared. See [docs/cli.md](../../docs/cli.md#identity-and-holder).

## Ask Kravis, do not copy it

Query `kravis schema <type>` for a type's fields, the catalogs that apply to you (channels, purposes, task statuses with their category), the value used when a field is omitted, the default order, the compact view and the error codes. The types are `user`, `organization`, `person`, `interaction`, `task`, `deal`, `project` and `note`. Each property says which flag sets it. `kravis schema` alone prints every type. Never copy its values into your notes: they change with the workspace.

`kravis <type> list|get|create|update|delete` is the same for every type. Add `--fields <a,b>` or `--full` to widen what a command prints. A create that has a natural key returns `existing` when repeated: a person by email, an organization by domain, an interaction by an identifying reference such as the Message-ID, a task by its interaction and purpose. A task without `--interaction`, or an interaction without a reference, has none: after a lost answer, list before you create it again.

## Record an interaction and its follow-up

Three calls, each safe to repeat when the interaction carries its Message-ID.

1. Find or create the people: `kravis person create --name <name> --email <email>`. An email that exists returns that person. To say a person works at an organization, use `kravis person affiliation add <person> <organization> --title <text>`; never add the organization as a participant for that.
2. Record the interaction: `kravis interaction create --channel <channel> --occurred-at <time> --subject <subject> --participant <person> --ref <reference>`. Add `--summary-file -` and pipe the summary in. `--direction inbound --initiated-by <participant>` says the other side started it; never guess the initiator. An outbound one, which is the default, is `--direction outbound --initiated-by <user>`, the user who started it.
3. Create the follow-up from it: `kravis task create --title <title> --assignee <user> --interaction <interaction> --due <date>`. It has the purpose `follow-up` and there is one per interaction and purpose, so creating it again returns the same task.

### References

A `--ref <source>:<kind>:<id>[@<scope>]` ties the interaction to the item it came from, and an identifying reference recorded twice finds the same interaction. Record the Message-ID first: `email:message:<message-id>`, with no angle brackets. Add the provider's own IDs next to it: they do not identify, so a create with only those makes a second interaction when it is repeated after a lost answer: list before you create it again. How to get the Message-ID from Gmail, Outlook, IMAP and Apple Mail is in [references/message-id.md](references/message-id.md).

## Manage tasks

- `kravis task list` shows your holder's live tasks by due date. Narrow with `--assignee <user>`, `--category <category>`, `--overdue` or `--due-before <date>`, and add `--all` for the finished ones.
- `kravis task status <task> <status>` is the only way to change the status. `update` refuses it.
- `kravis task update <task> --due <date>` changes the rest. An empty value clears a field: `--agent ""`.
- A task has one assignee, a live human, and may have an agent that the assignee handles: `--agent <agent>`.

## Run a deal

- `kravis deal create --party <person or organization> --holder-role <party role> [--role <party role>] [--name <name>]` opens a deal at the first stage. A deal has no natural key: list with `kravis deal list --party <id>` before you create it again.
- `kravis deal stage <deal> <stage>` is the only way to move it, and a lost stage needs `--reason <lost reason>`. `kravis deal party add <deal> <party> --role <party role>` adds a party.
- Add the client's contact person as a party too, `kravis deal party add <deal> <person> --role client`, so that the person's briefing finds the deal.
- Link work to it: `--deal <deal>` on `kravis interaction create` and `kravis task create`.

## Projects

- `kravis project create --name <name> [--deal <deal>]` and `kravis project list --deal <deal>`. A task takes `--project <project>` or `--deal <deal>` as its parent, never both, fixed at creation.
- `kravis project close <project>` fails with `REFERENCED` while the project has live tasks. `--cancel-tasks` cancels them first; `kravis project reopen <project>` makes the project live again but leaves canceled tasks canceled: move them with `kravis task status`.
- `--related <id>` on a task or project adds a person, organization, deal or project that is not its parent.

## Notes

`kravis note create --subject <id> --text-file -` writes a note about people, organizations, deals or projects, with the text piped in. Repeat `--subject` for several. `kravis note update <note> --subject <id>` adds one, and `--remove-subject` removes one, never the last. A note that is no longer useful is deleted.

## Prepare a briefing

For "everything about Ben Carter": resolve him with `kravis person get <email or id>`, then ask each list for his records with `--limit 0`, which returns them all (a list stops at 50 otherwise).

1. `kravis interaction list --participant <person> --limit 0`: what happened, newest first.
2. `kravis note list --related-to <person> --limit 0`: what is known about him.
3. `kravis task list --related-to <person> --limit 0`: the follow-ups about him, including those that came from an interaction with him.
4. `kravis deal list --party <person> --limit 0`: his deals.

For an organization use the same commands with its ID. For a deal or project, `--related-to <deal>` on `task list` also finds the tasks under it; `note list` matches only the notes that name it as a subject. For "what is due today": `kravis task list --due-before <tomorrow> --assignee <user>` shows what is due today and what is overdue; add `--overdue` for the late ones alone. `--due-before` excludes its date, so for "the next N days" pass the day after the last one you want. Dates are in the workspace timezone.

## Measure outreach

For "what outreach did Ana do in November": the outbound interactions she started, in the month.

`kravis interaction list --direction outbound --initiated-by <user> --from 2026-11-01 --to 2026-11-30 --limit 0`

`total` counts interactions, not people: for "how many people", collect the person IDs in `participants` and count the distinct ones. `--to` includes its whole day. Add `--participant <person>` for one person and `--with-agents` to count what her agents started too. Inbound ones are never outreach, and an interaction without an initiator matches no `--initiated-by`.

## Correct a mistake

A record created under the wrong actor is removed and created again, with no trace left: `kravis interaction delete <interaction>`, `kravis task delete <task>` or `kravis note delete <note>`, then create it with the right `--actor`. A delete of something other records refer to fails with `REFERENCED` and lists them in `details.referrers`. A delete of an interaction that has a follow-up is refused with `REFERENCED`: delete the task, delete the interaction, then create both again. The new task takes the interaction's single deal or project as its parent. Deactivate a user with `kravis user update <user> --live=false`. Run `kravis check` when a workspace seems damaged: it lists findings and exits with code 6.

## Words

Use Kravis's words: an `assignee` answers for a task (never "owner"), its `agent` carries it out (never "delegate"), a `holder` is the own organization whose records they are, and `live` says a record is still in use.
