GO ?= go
.PHONY: all test build golden bench clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

# 生成三份样本、跑 GTD1 无损往返与熵/体积度量，产物见 testdata/ 与 artifacts/。
bench:
	$(GO) run ./cmd/bench

golden:
	$(GO) run ./ref

clean:
	rm -rf build
