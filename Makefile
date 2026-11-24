GO ?= go
.PHONY: all test build golden clean recon-report

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

# 约束最小二乘五抽头求解 + 同组图双线性色差对照，产物在 artifacts/recon/
recon-report:
	$(GO) run ./cmd/reconreport

clean:
	rm -rf build
