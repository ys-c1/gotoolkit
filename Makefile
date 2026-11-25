GO ?= go
.PHONY: all test build golden report clean

all: test

test:
	$(GO) vet ./...
	$(GO) test ./...

build:
	$(GO) build -o build/gtk ./cmd/gtk

golden:
	$(GO) run ./ref

# report 重新生成单调重建电平专题的全部实算产物。
report:
	mkdir -p artifacts/monolevel
	cp docs/monotone-levels.md artifacts/monolevel/REPORT.md
	$(GO) run ./cmd/levelreport > artifacts/monolevel/report.txt
	$(GO) run ./cmd/levelreport -md > artifacts/monolevel/summary_table.md
	$(GO) run ./cmd/levelreport -json > artifacts/monolevel/report.json
	$(GO) run ./cmd/levelreport -csv > artifacts/monolevel/summary.csv

clean:
	rm -rf build
