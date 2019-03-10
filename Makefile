SOURCES := $(wildcard *.go)
SOURCES += $(wildcard config/*.go)
SOURCES += $(wildcard util/*.go)
SOURCES += $(wildcard walk/*.go)

all: mr

clean:
	rm -f mr

mr: $(SOURCES)
	go build mr.go

.PHONY: all clean
