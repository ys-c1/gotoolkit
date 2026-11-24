GO ?= go
.PHONY: all test build golden luma clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

luma:
	$(GO) run ./cmd/lumareport -out artifacts/luma

clean:
	rm -rf build
