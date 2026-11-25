GO ?= go
.PHONY: all test build golden images analyze clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

# 生成三张基准图（内部测试图，逐字节确定）。
images:
	$(GO) run ./tools/genimages testdata/images

# 重跑分区分析并写出全部产物与图表。
analyze: build images
	@for n in split checker scene; do \
		./build/gtk analyze --in testdata/images/$$n.pgm \
			--mask artifacts/hybrid/$${n}_mask.pgm \
			--tsv artifacts/hybrid/$${n}_sweep.tsv \
			--blocks artifacts/hybrid/$${n}_blocks.tsv \
			--json artifacts/hybrid/$${n}_summary.json; \
	done
	python3 tools/charts/charts.py

clean:
	rm -rf build
