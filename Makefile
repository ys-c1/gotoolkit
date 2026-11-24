GO ?= go
.PHONY: all test build golden study clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

study:
	$(GO) run ./cmd/quantstudy
	python3 analysis/plot_quant_bias.py

clean:
	rm -rf build
