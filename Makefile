GO ?= go
.PHONY: all test build seamcheck constraints golden clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

seamcheck: build
	./build/gtk seamcheck

constraints: build
	./build/gtk constraints --fixture quadratic

golden:
	$(GO) run ./ref

clean:
	rm -rf build
