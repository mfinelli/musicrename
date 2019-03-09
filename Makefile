SOURCES := $(wildcard *.go)
SOURCES += $(wildcard config/*.go)

all: mr

mr: $(SOURCES)
	go build mr.go

.PHONY: all
