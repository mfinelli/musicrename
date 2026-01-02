set dotenv-load := false
set ignore-comments := true

[private]
default:
  @just --list

fmt:
  go fmt ./...

test:
  go test -v ./...
