GO := go

SOURCES := $(wildcard *.go)
SOURCES += $(wildcard cmd/*.go)
SOURCES += $(wildcard util/*.go)
SOURCES += $(wildcard walk/*.go)

all: mr

clean:
	rm -f mr

mr: $(SOURCES) go.mod go.sum
	$(GO) build -o $@ \
		-buildmode=pie \
		-trimpath \
		-mod=readonly \
		-ldflags "-s -w -linkmode=external" \
		main.go

.PHONY: all clean
