---
name: kravis
description: Use Kravis, a CRM for agents, to record interactions with people and organizations, follow them up with tasks, and keep the history in one workspace of JSON files. Use it when asked to log an email, meeting or call, to remember who was involved, to create or move a follow-up, or to see what is overdue.
---

# Kravis

Kravis is a command, `kravis`, over a workspace: a folder with one JSON file per record. Every command prints one JSON object on stdout, and an error prints one on stderr with a `code`, `details` and a `hint`. Read the `hint` and `details.allowed` before you retry.

## Find the workspace and the actor

- The workspace is `KRAVIS_WORKSPACE`, or the nearest folder with `kravis.json` above the current one. Outside any workspace every command fails with `STORAGE_UNAVAILABLE`. In a new folder run `kravis init --timezone <zone> --organization <name> --user <key>`.
- A write needs an actor, a live user: `--actor <user>`, then `KRAVIS_ACTOR`, then the default stored on this machine. `kravis whoami` shows who you are and for which holder. `kravis whoami --set <user>` stores the default.
- A work record (interaction, task) belongs to the holder of the actor, and you read and write only your holder's. People, organizations and users are shared.

## Ask Kravis, do not copy it

Query `kravis schema <type>` for a type's fields, the catalogs that apply to you (channels, purposes, task statuses with their category), the value used when a field is omitted, the default order, the compact view and the error codes. The types are `user`, `organization`, `person`, `interaction` and `task`. Each property says which flag sets it. `kravis schema` alone prints every type. Never copy its values into your notes: they change with the workspace.

`kravis <type> list|get|create|update|delete` is the same for every type. Add `--fields <a,b>` or `--full` to widen what a command prints. A repeated create returns `existing` or `unchanged`, so repeating a call is safe.

## Record an interaction and its follow-up

Two calls, each safe to repeat.

1. Find or create the people: `kravis person create --name <name> --email <email>`. An email that exists returns that person.
2. Record the interaction: `kravis interaction create --channel <channel> --occurred-at <time> --subject <subject> --participant <person> --ref <reference>`. Add `--summary-file -` and pipe the summary in. `--direction inbound --initiated-by <participant>` says the other side started it; never guess the initiator.
3. Create the follow-up from it: `kravis task create --title <title> --assignee <user> --interaction <interaction> --due <date>`. It has the purpose `follow-up` and there is one per interaction and purpose, so creating it again returns the same task.

### References

A `--ref <source>:<kind>:<id>[@<scope>]` ties the interaction to the item it came from, and the same reference recorded twice finds the same interaction. Record the Message-ID first: `email:message:<message-id>`, with no angle brackets. Add the provider's own IDs next to it. How to get the Message-ID from Gmail, Outlook, IMAP and Apple Mail is in [references/message-id.md](references/message-id.md).

## Manage tasks

- `kravis task list` shows your holder's live tasks by due date. Narrow with `--assignee <user>`, `--category <category>`, `--overdue` or `--due-before <date>`, and add `--all` for the finished ones.
- `kravis task status <task> <status>` is the only way to change the status. `update` refuses it.
- `kravis task update <task> --due <date>` changes the rest. An empty value clears a field: `--agent ""`.
- A task has one assignee, a live human, and may have an agent that the assignee handles: `--agent <agent>`.

## Correct a mistake

A record created under the wrong actor is removed and created again, with no trace left: `kravis interaction delete <interaction>` or `kravis task delete <task>`, then create it with the right `--actor`. A delete of something other records refer to fails with `REFERENCED` and lists them in `details.referrers`. Deactivate a user with `kravis user update <user> --live=false`. Run `kravis check` when a workspace seems damaged: it lists findings and exits with code 6.

## Words

Use Kravis's words: an `assignee` answers for a task (never "owner"), its `agent` carries it out (never "delegate"), a `holder` is the own organization whose records they are, and `live` says a record is still in use.
