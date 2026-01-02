set dotenv-load := false
set ignore-comments := true

[private]
default:
  @just --list

[private]
build:
  make mr

[working-directory: 'tmp']
e2e: e2e-setup build
  ../mr rename

fmt:
  go fmt ./...

test:
  go test -v ./...

[private]
e2e-setup:
  @mkdir -p tmp

  @mkdir -p "tmp/AC_DC"
  @mkdir -p "tmp/AC_DC/[1980] Back In Black"
  @mkdir -p "tmp/AC_DC/[1979] Highway To Hell"
