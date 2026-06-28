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

all: mrr mrr.1 mrr.bash mrr.fish mrr.zsh

clean:
	rm -f mrr mrr.1 mrr.bash mrr.fish mrr.zsh

mrr: $(SOURCES) go.mod go.sum
	$(GO) build -o $@ \
		-buildmode=pie \
		-trimpath \
		-mod=readonly \
		-ldflags "-s -w -linkmode=external" \
		main.go

mrr.bash: mrr
	./$< completion bash > $@

mrr.fish: mrr
	./$< completion fish > $@

mrr.zsh: mrr
	./$< completion zsh > $@

mrr.1: mrr.1.scd
	sed -e "s/__VERSION__/$(VERSION)/" -e "s/__DATE__/$(TODAY)/" \
		$< | scdoc > $@

install: all
	install -Dm0755 mrr "$(DESTDIR)$(PREFIX)/bin/mrr"
	install -Dm0644 README.md \
		"$(DESTDIR)$(PREFIX)/share/doc/musicrename/README.md"
	install -Dm0644 mrr.bash \
		"$(DESTDIR)$(PREFIX)/share/bash-completion/completions/mrr"
	install -Dm0644 mrr.fish \
		"$(DESTDIR)$(PREFIX)/share/fish/vendor_completions.d/mrr.fish"
	install -Dm0644 mrr.zsh \
		"$(DESTDIR)$(PREFIX)/share/zsh/site-functions/_mrr"
	install -Dm0644 mrr.1 "$(DESTDIR)$(PREFIX)/share/man/man1/mrr.1"

uninstall:
	rm -rf "$(DESTDIR)$(PREFIX)/bin/mrr" \
		"$(DESTDIR)$(PREFIX)/share/doc/musicrename/README.md" \
		"$(DESTDIR)$(PREFIX)/share/bash-completion/completions/mrr" \
		"$(DESTDIR)$(PREFIX)/share/fish/vendor_completions.d/mrr.fish" \
		"$(DESTDIR)$(PREFIX)/share/zsh/site-functions/_mrr" \
		"$(DESTDIR)$(PREFIX)/share/man/man1/mrr.1"

.PHONY: all clean install uninstall
