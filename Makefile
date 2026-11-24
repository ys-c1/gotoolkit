GO ?= go
.PHONY: all test build analyze golden clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

analyze:
	$(GO) run ./cmd/sbanalyze --out artifacts/subband

golden:
	$(GO) run ./ref

clean:
	rm -rf build
