GO ?= go
.PHONY: all test build golden clean study

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

study:
	$(GO) run ./cmd/gtk study --outdir artifacts/deblock

golden:
	$(GO) run ./ref

clean:
	rm -rf build
