set dotenv-load := false
set ignore-comments := true

[private]
default:
    @just --list

# Update all versions strings to "v"
bump v:
    sed -i -E "s|(LABEL org\.opencontainers\.image\.version=v).*|\1{{ v }}|" \
        Dockerfile
    sed -i -E "s|(Version:\s+\").*(\",)|\1{{ v }}\2|" cmd/root.go
