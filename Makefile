GO ?= go
.PHONY: all test build seamcheck golden clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

seamcheck: build
	./build/gtk seamcheck

golden:
	$(GO) run ./ref

clean:
	rm -rf build
