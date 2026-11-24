GO ?= go
.PHONY: all test build golden roundtrip clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

# 三格式往返报告：每格式一份 + 合并总判定 + 往返前后两张图，落在 artifacts/roundtrip/。
roundtrip:
	$(GO) run ./cmd/roundtrip -out artifacts/roundtrip

clean:
	rm -rf build
