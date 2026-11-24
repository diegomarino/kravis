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
