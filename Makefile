GO ?= go
.PHONY: all test build golden roundtrip clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

roundtrip:
	python3 scripts/gray_roundtrip.py

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

clean:
	rm -rf build
