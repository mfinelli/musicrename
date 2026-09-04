set dotenv-load := false
set ignore-comments

sed := if os() == "macos" { "gsed" } else { "sed" }

[private]
default:
    @just --list

# Update all versions strings to "v"
bump v:
    {{ sed }} -i -E \
        "s|(LABEL org\.opencontainers\.image\.version=v).*|\1{{ v }}|" \
        Dockerfile
    {{ sed }} -i -E "s|(Version:\s+\").*(\",)|\1{{ v }}\2|" cmd/root.go

# Error if testutil is used outside test code
check-testutil:
    #!/usr/bin/env bash
    set -euo pipefail
    bad=$(grep -rl '"github.com/mfinelli/musicrename/internal/testutil"' \
        --include='*.go' . \
        | grep -v '_test\.go$' \
        | grep -v '^\./internal/testutil/' || true)
    if [ -n "$bad" ]; then
        echo "internal/testutil must only be imported from _test.go files:"
        echo "$bad"
        exit 1
    fi

# Formats all files (requires prettier)
fmt:
    go fmt ./...
    prettier -w *.md
    just --fmt
