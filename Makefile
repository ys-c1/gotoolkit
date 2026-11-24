GO ?= go
.PHONY: all test build golden check clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

# 十二组缩略图链路端到端检查（帮助、两列对照表、错误码、全关字节不变）。
check: build
	bash scripts/thumb_pipeline_check.sh

golden:
	$(GO) run ./ref

clean:
	rm -rf build
