# Development

Stack, repository layout, tooling and the definition of done for the MVP.

## Stack

- **Go.** Code, commands, field names, error codes and messages are in English, using US spelling.
- **Library first.** All domain logic (validation, rules, writes) lives in a Go library. The CLI is a thin layer over it, built with cobra in `gh` style, and a future MCP server will be another thin layer over the same library. No surface carries logic of its own.
- **Schemas.** Annotated Go types are the source of truth. `kravis schema` is generated with `invopop/jsonschema`, and the workspace catalogs are injected at run time. Input is validated in Go with Kravis's own errors (`details`, `hint`). Rules that span records are domain logic.
- **Few dependencies.** The standard library first. The planned dependencies are cobra and `invopop/jsonschema`; any other needs a reason in its commit. Errors are Kravis's own types built with the standard `errors` package (`%w`, `errors.Is`, `errors.As`), with no third-party error library. Tests use the standard `testing` package.
- **Two decoding modes.** Agent input is decoded strictly and refuses unknown fields. Records read from disk are decoded tolerantly, and their unknown fields are kept when the record is rewritten.

## Repository layout

```text
cmd/kravis/            CLI (cobra), thin
internal/domain/       records, rules, validation, catalogs
internal/storage/      adapter (get, create, replace, list) and the local filesystem connector
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

### Tests that guard the contract

- **Schema matches validation.** For each type, values accepted by the generated JSON Schema are accepted by the Go validation, and values it rejects are rejected.
- **Acceptance criteria.** Every acceptance criterion in the ddduck model that is in scope for the increment has an end-to-end test. The test runs the built binary against a temporary workspace and checks the JSON output and the exit code.
- **The skill does not drift.** A test extracts every command the skill cites, meaning every code block or inline code that starts with `kravis`. It takes the words up to the first argument that starts with `-` or `<` or is quoted, and compares them, with their flags, against the command tree that a hidden command of the binary prints. A command or flag that the binary lacks fails the build.
- **The skill installs.** CI installs the skill from the checkout into a temporary folder with `npx skills add` and checks that it is found with valid frontmatter.

## CI and releases

- **`ci.yml`** runs on every push and pull request: format check, lint, tests, build and `ddduck check`, plus the commit subject check on pull requests. The last one makes CI fail if the model in `docs/ddd/` becomes invalid or its generated views are stale.
- **`release.yml`** runs on a version tag. goreleaser builds binaries for macOS, Linux and Windows and publishes them as GitHub Release assets.

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
