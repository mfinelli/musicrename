SOURCES := $(wildcard *.go)
SOURCES += $(wildcard cmd/*.go)
SOURCES += $(wildcard uploader/*.go)
SOURCES += $(wildcard util/*.go)

all: mr

clean:
	rm -f mr

mr: $(SOURCES)
	go build -o $@ main.go

.PHONY: all clean
