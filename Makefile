GO ?= go
.PHONY: all test build golden clean chain

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

chain:
	$(GO) run ./cmd/chainbench

golden:
	$(GO) run ./ref

clean:
	rm -rf build
