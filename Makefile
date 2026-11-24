GO ?= go
.PHONY: all test build golden lab clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

lab:
	$(GO) run ./cmd/reorderlab -out artifacts/reorder

clean:
	rm -rf build
