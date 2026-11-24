GO ?= go
.PHONY: all test build golden clean motion motion-test

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

motion:
	python3 scripts/motion_lab.py

motion-test:
	python3 scripts/test_motion_lab.py

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

clean:
	rm -rf build
