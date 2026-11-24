GO ?= go
.PHONY: all test build golden clean lab

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

lab:
	$(GO) run ./cmd/corruption-lab --out artifacts/corruption-lab

golden:
	$(GO) run ./ref

clean:
	rm -rf build
