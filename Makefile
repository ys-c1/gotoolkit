GO ?= go
.PHONY: all test build golden goldenreport clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

# 生成锐化反解与实测过冲的报告（docs/sharpen-overshoot.md）。
goldenreport:
	$(GO) run ./cmd/sharpreport

clean:
	rm -rf build
