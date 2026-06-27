PREFIX := /usr/local
DESTDIR :=

GO := go
GREP := grep

ifeq ($(shell uname), Darwin)
        GREP := ggrep
endif

SOURCES := $(wildcard *.go cmd/*.go internal/checker/*.go \
	   internal/executor/*.go internal/hasher/*.go internal/lyrics/*.go \
	   internal/metadata/*.go internal/planner/*.go internal/sanitize/*.go)

VERSION ?= $(shell $(GREP) -P "^\tVersion:" cmd/root.go | awk -F\" '{print $$2}')
TODAY ?= $(shell date +%Y-%m-%d)

all: mr mr.1 mr.bash mr.fish mr.zsh

clean:
	rm -f mr mr.1 mr.bash mr.fish mr.zsh

mr: $(SOURCES) go.mod go.sum
	$(GO) build -o $@ \
		-buildmode=pie \
		-trimpath \
		-mod=readonly \
		-ldflags "-s -w -linkmode=external" \
		main.go

mr.bash: mr
	./$< completion bash > $@

mr.fish: mr
	./$< completion fish > $@

mr.zsh: mr
	./$< completion zsh > $@

mr.1: mr.1.scd
	sed -e "s/__VERSION__/$(VERSION)/" -e "s/__DATE__/$(TODAY)/" \
		$< | scdoc > $@

install: all
	install -Dm0755 mr "$(DESTDIR)$(PREFIX)/bin/mr"
	install -Dm0644 README.md \
		"$(DESTDIR)$(PREFIX)/share/doc/musicrename/README.md"
	install -Dm0644 mr.bash \
		"$(DESTDIR)$(PREFIX)/share/bash-completion/completions/mr"
	install -Dm0644 mr.fish \
		"$(DESTDIR)$(PREFIX)/share/fish/vendor_completions.d/mr.fish"
	install -Dm0644 mr.zsh \
		"$(DESTDIR)$(PREFIX)/share/zsh/site-functions/_mr"
	install -Dm0644 mr.1 "$(DESTDIR)$(PREFIX)/share/man/man1/mr.1"

uninstall:
	rm -rf "$(DESTDIR)$(PREFIX)/bin/mr" \
		"$(DESTDIR)$(PREFIX)/share/doc/musicrename/README.md" \
		"$(DESTDIR)$(PREFIX)/share/bash-completion/completions/mr" \
		"$(DESTDIR)$(PREFIX)/share/fish/vendor_completions.d/mr.fish" \
		"$(DESTDIR)$(PREFIX)/share/zsh/site-functions/_mr" \
		"$(DESTDIR)$(PREFIX)/share/man/man1/mr.1"

.PHONY: all clean install uninstall
