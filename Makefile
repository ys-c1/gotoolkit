GO ?= go
.PHONY: all test build golden clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

clean:
	rm -rf build

# 低位深组合矩阵：需要两档参数各跑一次，产物落在 artifacts/lowbit/。
matrix: build
	./build/gtk matrix --in testdata/input/gradient.pgm --out artifacts/lowbit/matrix
	./build/gtk dump --in testdata/input/gradient.pgm --bits 4 > artifacts/lowbit/dump-bits4.txt
	./build/gtk dump --in testdata/input/gradient.pgm --bits 2 --target-err 43 > artifacts/lowbit/dump-bits2.txt
