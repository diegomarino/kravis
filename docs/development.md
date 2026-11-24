# Development

Stack, repository layout, tooling and the definition of done for the MVP.

## Stack

- **Go.** Code, commands, field names, error codes and messages are in English, using US spelling.
- **Library first.** All domain logic (validation, rules, writes) lives in a Go library. The CLI is a thin layer over it, built with cobra in `gh` style, and a future MCP server will be another thin layer over the same library. No surface carries logic of its own.
- **Schemas.** Annotated Go types are the source of truth. `kravis schema` is generated with `invopop/jsonschema`, and the workspace catalogs are injected at run time. Input is validated in Go with Kravis's own errors (`details`, `hint`). Rules that span records are domain logic.
- **Few dependencies.** The standard library first: `log/slog` for logs (on stderr, only with `--verbose` or `KRAVIS_LOG=debug`), `text/tabwriter` for TTY tables, `time` with `time/tzdata` embedded so zones work on every OS. The only other dependencies are:
  - `github.com/spf13/cobra` for the CLI;
  - `github.com/invopop/jsonschema` for `kravis schema`;
  - `go.jetify.com/typeid` for TypeIDs;
  - `golang.org/x/text` for slugs without accents;
  - `golang.org/x/term` to detect a TTY;
  - `github.com/google/go-cmp`, in tests only.

  Any other needs a reason in its commit. Errors are Kravis's own types built with the standard `errors` package (`%w`, `errors.Is`, `errors.As`), with no third-party error library. Tests use the standard `testing` package.
- **Two decoding modes.** Agent input is decoded strictly and refuses unknown fields. Records read from disk are decoded tolerantly, and their unknown fields are kept when the record is rewritten.

## Repository layout

```text
cmd/kravis/            CLI (cobra), thin
internal/domain/       records, rules, validation, catalogs
internal/storage/      adapter (get, create, replace, delete, list) and the local filesystem connector
internal/schema/       JSON Schema generation and catalog injection
skills/kravis/         operational skill (SKILL.md, references/ only if it grows)
docs/                  storage.md, cli.md, development.md
docs/ddd/              ddduck product model (canonical YAML, ADRs, generated views)
justfile
.githooks/             commit-msg
.golangci.yml
.goreleaser.yaml
.github/workflows/     ci.yml, release.yml
```

The Go module path is `github.com/diegomarino/kravis`.

## Tooling

| Task | Tool | Command |
|---|---|---|
| Format | `gofmt` | `just fmt` |
| Lint | `golangci-lint` (default linters, plus `gofmt` and `misspell`) | `just lint` |
| Test | `go test ./...` | `just test` |
| Build | `go build ./cmd/kravis` | `just build` |
| Model | `ddduck check --root docs/ddd` | `just check` |
| Git hooks | `.githooks/` via `core.hooksPath` | `just hooks` |

### Commits

- Commit subjects follow [Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>)!: <summary>`, with the scope and `!` optional, in English and at most 72 characters.
- Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`, `chore`, `revert`. A breaking change of the CLI contract or the storage format carries `!`.
- Scopes are record types or areas when they help: `task`, `interaction`, `storage`, `cli`, `schema`, `skill`, `model`.
- `just hooks` installs a `commit-msg` hook that checks the subject locally. CI checks it again on every pull request, and goreleaser groups the changelog by type.

### Rules every test follows

- **Nothing personal in the repository.** A test scans every tracked file and fails on a home path (`/Users/<name>`, `/home/<name>`, `C:\Users\<name>`) or an email address outside `example.com`, `example.org` and `example.net`. Fixtures use invented names.
- **Tests never touch a real workspace or configuration.** Every test runs through one helper that gives it a workspace in `t.TempDir()`, a throwaway `HOME` and `XDG_CONFIG_HOME`, and no inherited `KRAVIS_*` variables, so the developer's own actor and workspaces are never read or written.
- **`just test` stays fast**, a matter of seconds, so it can run at every step. A slow test needs a reason.

### Tests that guard the contract

- **Schema matches validation.** For each type, values accepted by the generated JSON Schema are accepted by the Go validation, and values it rejects are rejected.
- **Acceptance criteria.** Every acceptance criterion in the ddduck model that is in scope for the increment has an end-to-end test whose name cites its ID, for example `TestWORKSPACE_AC_04`. A test reads the in-scope criteria from the model and fails if any of them has no test. The tests call the CLI in process, through `run(args, stdin, stdout, stderr) int`, against a temporary workspace, and check the JSON output and the exit code, so they count towards coverage. One smoke test runs the built binary.
- **Minimums.** `just test` runs every test with the race detector and fails below 80% coverage of all the code. `just lint` reports no findings.
- **The skill does not drift.** A test extracts every command the skill cites, meaning every code block or inline code that starts with `kravis`. It takes the words up to the first argument that starts with `-` or `<` or is quoted, and compares them, with their flags, against the command tree that a hidden command of the binary prints. A command or flag that the binary lacks fails the build.
- **The skill installs.** CI installs the skill from the checkout into a temporary folder with `npx skills add` and checks that it is found with valid frontmatter.

## CI and releases

- **`ci.yml`** runs on every push and pull request: format check, lint, tests, build, `govulncheck` and `ddduck check`, plus the commit subject check on pull requests. The last one makes CI fail if the model in `docs/ddd/` becomes invalid or its generated views are stale.
- **`release.yml`** runs on a version tag. goreleaser builds binaries for macOS, Linux and Windows and publishes them as GitHub Release assets.

## Autonomy

Agents implement on their own within the spec. Only decisions that change what was already agreed wait for the person.

- **Changes to `main`** arrive only through pull requests with CI green; there are no direct pushes. Tags `v*` are created only by the person.
- **The spec gate** is a CI job that classifies each pull request:
  - **Additive** (merges on its own once CI and the review pass): `ddduck diff` against the base shows only added records, or changes to concepts and use cases that add text without removing any; `GLOSSARY.md`, `docs/cli.md` and `docs/storage.md` gain lines without losing any; no commit carries `!`.
  - **Needs the person** (the job fails until the person adds the `spec-approved` label): `ddduck diff` shows a removed or relocated record, or a changed guarantee or relationship; any file under `docs/ddd/decisions/` is added or changed; `GLOSSARY.md`, `docs/cli.md` or `docs/storage.md` lose or change a line; a contract fixture changes (the output of `kravis schema`, golden record files); or a commit carries `!`.
- **Issues** are implemented only when labelled `ready`, which the person adds once the issue's behavior is in the model. For an issue without it, an agent may only open a pull request with the proposed spec change, which goes through the spec gate like any other.

## Installation

- `go install github.com/diegomarino/kravis/cmd/kravis@latest`, or download a binary from GitHub Releases.
- The skill: `npx skills add diegomarino/kravis --skill kravis`.
- Homebrew and npm are later improvements.

## Definition of done for the MVP

- The behavior: every use case and guarantee in scope in the ddduck model, with the acceptance tests passing.
- Developer experience: format, lint, tests, build, model check, commit convention and release automated through `just`, the git hooks and CI.
- The skill installs with `npx skills`, cites only commands and flags that exist, and teaches agents to query `kravis schema` rather than copy it.
- Quality docs:
  - a README with what Kravis is, how to install it and a five-minute quickstart (`init`, a user, an interaction, a follow-up);
  - `docs/storage.md`, `docs/cli.md` and this file, kept true to the code;
  - the ddduck model with fresh generated views.
