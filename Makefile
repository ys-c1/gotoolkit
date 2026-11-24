GO ?= go
.PHONY: all test build analyze golden clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

analyze:
	$(GO) run ./cmd/analyze --outdir artifacts/roundtrip

golden:
	$(GO) run ./ref

clean:
	rm -rf build
