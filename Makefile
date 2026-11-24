GO ?= go
.PHONY: all test build golden clean joint

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

joint:
	$(GO) run ./cmd/jointbench

golden:
	$(GO) run ./ref

clean:
	rm -rf build
