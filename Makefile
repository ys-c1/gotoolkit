GO ?= go
.PHONY: all test build golden guard clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

guard:
	$(GO) run ./cmd/guard

clean:
	rm -rf build
