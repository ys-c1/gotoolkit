GO ?= go
.PHONY: all test build golden clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

# quantexp：三档位深量化误差熵实验，产物写到 artifacts/
quantexp:
	$(GO) run ./cmd/quantexp --out artifacts

golden:
	$(GO) run ./ref

clean:
	rm -rf build
