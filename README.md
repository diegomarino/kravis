# Kravis

A CRM for agents. Agents and the people they work for use it to record and follow relationships and work, in files they own: one JSON file per record, in a folder you can read, diff and keep in git.

Every command prints one JSON object, so an agent can call it directly. Today it covers people, organizations, users, interactions (emails, meetings, calls) and the tasks that follow from them.

## Install

```sh
go install github.com/diegomarino/kravis/cmd/kravis@latest
```

Or download a binary for macOS, Linux or Windows from [Releases](https://github.com/diegomarino/kravis/releases). Check it with `kravis --version`.

To teach your agent the commands, install the skill:

```sh
npx skills add diegomarino/kravis --skill kravis
```

## Five-minute quickstart

In an empty folder:

```sh
kravis init --timezone Europe/Madrid --organization "Acme Advisory" --user ana --user-name "Ana Ruiz"
kravis user create "scout" --agent --holder org_acme-advisory --handler us_ana --name "Scout"
kravis person create --name "Ben Carter" --email ben@example.com
kravis interaction create --channel email --occurred-at 2026-11-23T10:00:00Z --subject "Intro call" --participant ben@example.com --ref email:message:intro-001@example.com
kravis task create --title "Send the proposal" --assignee us_ana --interaction it_2026-11-23-intro-call --due 2026-12-01
kravis task list
```

`init` creates the workspace, your organization and you as its first user. The second line adds an agent that works under you. Then you record an interaction with a person and create the follow-up it calls for. `task list` shows it, by due date.

## More

- [`docs/cli.md`](docs/cli.md): the commands, output, errors and exit codes.
- [`docs/storage.md`](docs/storage.md): the files on disk.
- [`docs/development.md`](docs/development.md): stack, tooling and releases.
- [`GLOSSARY.md`](GLOSSARY.md): the words Kravis uses.
