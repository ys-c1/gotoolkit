GO ?= go
.PHONY: all test build golden targets clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

# 四档目标体积反解对账（内置 256x256 确定性图），产出 artifacts/target-records.jsonl 与各档 .blk/.pgm
targets: build
	./build/gtk targets --no-color | tee artifacts/target-report.txt

golden:
	$(GO) run ./ref

clean:
	rm -rf build
