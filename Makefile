SOURCES := $(wildcard *.go)
SOURCES += $(wildcard cmd/*.go)

all: mr

clean:
	rm -f mr

mr: $(SOURCES)
	go build -o $@ main.go

.PHONY: all clean
