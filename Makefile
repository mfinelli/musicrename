GO := go

SOURCES := $(wildcard *.go cmd/*.go internal/checker/*.go \
	   internal/executor/*.go internal/hasher/*.go internal/lyrics/*.go \
	   internal/metadata/*.go internal/planner/*.go internal/sanitize/*.go)

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
