GO ?= go
.PHONY: all test build golden clean bench

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

bench: build
	./build/gtk bench --thresholds 4,7 --out artifacts/bench

golden:
	$(GO) run ./ref

clean:
	rm -rf build
