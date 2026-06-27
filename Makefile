PREFIX := /usr/local
DESTDIR :=

GO := go

SOURCES := $(wildcard *.go cmd/*.go internal/checker/*.go \
	   internal/executor/*.go internal/hasher/*.go internal/lyrics/*.go \
	   internal/metadata/*.go internal/planner/*.go internal/sanitize/*.go)

all: mr mr.bash mr.fish mr.zsh

clean:
	rm -f mr mr.bash mr.fish mr.zsh

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

.PHONY: all clean
