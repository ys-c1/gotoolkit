# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

低比特量化（`--bits 1..8`，默认不量化）的单调性约束、修前非单调样例与
逐点实测见 [`docs/monotonic-quant.md`](docs/monotonic-quant.md)；
`go run ./cmd/monoreport` 可复现三档位深（2/3/4 bit）的破坏点数与最大反转量。
