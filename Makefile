GO ?= go
.PHONY: all test build golden fixtures clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

# 头结构支路验收样例（含三个字段冲突样例），生成到 testdata/pgmx。
fixtures:
	$(GO) run ./cmd/genpgmx

clean:
	rm -rf build
