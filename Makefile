GO ?= go
.PHONY: all test build golden samples layoutcheck clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	cd ref && $(GO) run .

samples:
	$(GO) run ./cmd/mksamples -out testdata/samples

layoutcheck:
	$(GO) run ./cmd/layoutcheck -samples testdata/samples -out artifacts/layoutcheck

clean:
	rm -rf build
