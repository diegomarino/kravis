# Development

Stack, repository layout, tooling and the definition of done for the MVP.

## Start here

1. **Checkout:** `git clone https://github.com/diegomarino/kravis && cd kravis`.
2. **Platforms:** macOS and Linux (the two CI runs and the release builds). Windows is not supported.
3. **Tools:** Go, Node.js, `just`, `golangci-lint` and `ddduck`. Their pins are the source of truth, so read them there and install those versions with your usual package manager (`brew`, `apt`, `mise`, ...):
   - Go: the `go` line of [`go.mod`](../go.mod).
   - Node.js, `just`, `golangci-lint` and `ddduck`: [`ci.yml`](../.github/workflows/ci.yml) (`node-version`, `just-version`, `GOLANGCI_LINT_VERSION`, `DDDUCK_VERSION`). `ddduck` is an npm package: `npm install --global ddduck@<version>`.
4. **Verify:** `go version`, `node --version`, `just --version`, `golangci-lint --version` and `ddduck --version` each print the pinned version.
5. **First run:** `just hooks`, then `just build`, `just test`, `just lint` and `just check`. All four must pass before a change is ready.

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
internal/workspace/    finding the workspace a command runs on, and the local configuration
internal/repocheck/    whole-repository tests: nothing personal, the scope inventory
internal/specgate/     the spec gate's classification of a pull request (cmd/specgate runs it)
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
- **Acceptance criteria.** Every acceptance criterion in the ddduck model that is in scope for the increment has an end-to-end test whose name cites its ID, for example `TestWORKSPACE_AC_04`. The in-scope criteria and invariants are listed in `internal/repocheck/scope.txt`; a test checks that each exists in the model and fails if any of them has no test, and another pins the list to the increment's scope. The tests call the CLI in process, through `run(args, stdin, stdout, stderr) int`, against a temporary workspace, and check the JSON output and the exit code, so they count towards coverage. One smoke test runs the built binary.
- **Minimums.** `just test` runs every test with the race detector and fails below 80% coverage of all the code. `just lint` reports no findings.
- **The skill does not drift.** A test extracts every command the skill cites, meaning every code block or inline code that starts with `kravis`. It takes the words up to the first argument that starts with `-` or `<` or is quoted, and compares them, with their flags, against the command tree that a hidden command of the binary prints. A command or flag that the binary lacks fails the build.
- **The skill installs.** CI installs the skill from the checkout into a temporary folder with `npx skills add` and checks that it is found with valid frontmatter.

## CI and releases

- **`ci.yml`** runs on pushes to `main` and on pull requests: format check, lint, tests, build, `govulncheck` and `ddduck check`, on Linux and macOS, plus the commit subject check and the spec gate on pull requests. The last one makes CI fail if the model in `docs/ddd/` becomes invalid or its generated views are stale.
- **`release.yml`** runs on pushes to `main`. release-please keeps one release PR open with the next version and its changelog, built from the Conventional Commit subjects since the last release (`release-please-config.json`, `.release-please-manifest.json`; while the version is below 1.0 a `feat` bumps the minor and a `fix` the patch). Merging that PR creates the tag and the GitHub release, and the same workflow then runs goreleaser, which builds binaries for macOS and Linux and adds them and `checksums.txt` to that release. goreleaser writes no changelog: the notes are release-please's. The build is in this workflow, not a tag-triggered one, because a tag created with `GITHUB_TOKEN` starts no other workflow. For the same reason the release PR runs no checks unless the repository secret `RELEASE_PLEASE_TOKEN` (a PAT or app token) is set; without it, `GITHUB_TOKEN` is used and the PR needs its checks run another way.
- **To release**, merge the release PR; that merge is the person's yes. Each increment is released by merging it at the end of the increment. Pull request titles start with the Conventional Commit type (no ticket prefix): release-please reads the squash commit, which takes the title.

## Platforms

- macOS is the main platform. Linux is built and CI runs every test on Ubuntu, but it is not used day to day.
- Before 1.0 there is no automatic upgrade of a workspace's format from older releases. A release that changes it says so in its notes.

## Autonomy

Agents implement on their own within the spec. Only decisions that change what was already agreed wait for the person.

- **Changes to `main`** arrive only through pull requests with CI green; there are no direct pushes. Tags `v*` come only from merging the release PR, which is the person's call.
- **The spec gate** is a CI job that classifies each pull request:
  - **Additive** (merges on its own once CI and the review pass): `ddduck diff` against the base shows only added records, or changes to concepts and use cases that add text without removing any; `GLOSSARY.md`, `docs/cli.md` and `docs/storage.md` gain lines without losing any; no commit carries `!`.
  - **Needs the person** (the job fails until the person adds the `spec-approved` label): `ddduck diff` shows a removed or relocated record, or a changed guarantee or relationship; any file under `docs/ddd/decisions/` is added or changed; `GLOSSARY.md`, `docs/cli.md` or `docs/storage.md` lose or change a line; a contract fixture changes (the output of `kravis schema`, golden record files); or a commit carries `!`.
- **Issues** are implemented only when labelled `ready`, which the person adds once the issue's behavior is in the model. For an issue without it, an agent may only open a pull request with the proposed spec change, which goes through the spec gate like any other.

## Installation

- `go install github.com/diegomarino/kravis/cmd/kravis@latest`, or download a binary from GitHub Releases.
- The skill, of the same release as the binary: `npx skills add https://github.com/diegomarino/kravis/tree/v0.2.0/skills/kravis` <!-- x-release-please-version -->. release-please rewrites the version on the marked lines of `README.md` and this file on every release (`extra-files` in `release-please-config.json`).
- Homebrew and npm are later improvements.

## Definition of done for the MVP

- The behavior: every use case and guarantee in scope in the ddduck model, with the acceptance tests passing.
- Developer experience: format, lint, tests, build, model check, commit convention and release automated through `just`, the git hooks and CI.
- The skill installs with `npx skills`, cites only commands and flags that exist, and teaches agents to query `kravis schema` rather than copy it.
- Quality docs:
  - a README with what Kravis is, how to install it and a five-minute quickstart (`init`, a user, an interaction, a follow-up);
  - `docs/storage.md`, `docs/cli.md` and this file, kept true to the code;
  - the ddduck model with fresh generated views.
