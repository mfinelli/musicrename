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
  @rm -rf tmp
  @mkdir -p tmp

  @mkdir -p "tmp/AC_DC"
  @mkdir -p "tmp/AC_DC/[1980] Back In Black"
  @touch "tmp/AC_DC/[1980] Back In Black/01 Hells Bells.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/02 Shoot to Thrill.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/03 What Do You Do for Money Honey.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/04 Given the Dog a Bone.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/05 Let Me Put My Love Into You.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/06 Back in Black.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/07 You Shook Me All Night Long.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/08 Have a Drink on Me.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/09 Shake a Leg.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/10 Rock and Roll Ain't Noise Pollution.flac"
  @touch "tmp/AC_DC/[1980] Back In Black/folder.jpg"
  @mkdir -p "tmp/AC_DC/[1979] Highway To Hell"

  @mkdir -p "tmp/Beyoncé"
