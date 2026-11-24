GO ?= go
.PHONY: all test build golden chroma-report clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

# 生成三档彩图的双线性/自适应色度重建色差对照与 PPM 产物。
chroma-report:
	$(GO) run ./cmd/chromareport

clean:
	rm -rf build
