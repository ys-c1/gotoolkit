GO ?= go
.PHONY: all test build golden clean batch-e2e

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

# 批量链路端到端：十二组各跑一次，机读记录落在 artifacts/batch-records/。
batch-e2e:
	bash script/batch-e2e.sh

clean:
	rm -rf build
