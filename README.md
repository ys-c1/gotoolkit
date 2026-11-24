# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `internal/quant/` 调色板量化与有序抖动，色块/噪声误差拆分与最优配比
- `docs/dither.md` 误差拆分、4:1 权重依据、三档桶数的最优配比与误差
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
./build/gtk encode --format pgm --in a.pgm --out a.pgm --levels 16 --auto-dither
```
