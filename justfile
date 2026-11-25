# Install the repository's git hooks.
hooks:
    git config core.hooksPath "{{justfile_directory()}}/.githooks"

# Check the ddduck product model and its generated views.
check:
    ddduck check --root docs/ddd

# Format all Go code.
fmt:
    gofmt -w .

# Lint: the default linters plus gofmt and misspell.
lint:
    golangci-lint run

# Run every test with the race detector; fail below 80% coverage of all the code.
test:
    go test -race -coverpkg=./... -coverprofile=coverage.out ./...
    @go tool cover -func=coverage.out | awk '/^total:/ { gsub("%","",$3); printf "total coverage: %s%%\n", $3; if ($3+0 < 80) { print "coverage is below 80%"; exit 1 } }'

# Build the CLI into bin/, with the version from git (dev without a tag).
build:
    go build -ldflags "-X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/kravis ./cmd/kravis

# Regenerate THIRD_PARTY_NOTICES.md from the modules compiled into the binary.
notices:
    #!/usr/bin/env bash
    set -euo pipefail
    {
      echo "# Third-party notices"
      echo
      echo "Kravis includes the Go standard library and the modules below."
      echo "Regenerate with \`just notices\`; a test fails when this file is out of date."
      echo
      echo "## Go standard library"
      echo "The Go Authors license, the same text as golang.org/x below:"
      echo
      echo '```'; cat "$(go list -m -f '{{ "{{" }}.Dir}}' golang.org/x/sys)/LICENSE"; echo '```'
      for goos in darwin linux; do
        GOOS=$goos CGO_ENABLED=0 go list -deps -f '{{ "{{" }}with .Module}}{{ "{{" }}if not .Main}}{{ "{{" }}.Path}} {{ "{{" }}.Version}} {{ "{{" }}.Dir}}{{ "{{" }}end}}{{ "{{" }}end}}' ./cmd/kravis
      done | sort -u | while read -r path version dir; do
        echo; echo "## $path $version"
        for f in "$dir"/*; do
          case "$(basename "$f" | tr A-Z a-z)" in licen[cs]e*|copying*|notice*) [ -f "$f" ] || continue
            echo; echo "### $(basename "$f")"; echo; echo '```'; cat "$f"; echo '```';; esac
        done
      done
    } > THIRD_PARTY_NOTICES.md
