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

# 可选：中位切分调色板；不写 --palette 时产物与历史字节一致
./build/gtk encode --format rle --palette --buckets 16 --in a.pgm --out a.rle
go run ./cmd/palettecurve   # 生成桶数—误差曲线表与三档 PGM
```

调色板的桶数预测、三档实测误差和分裂顺序对照见
[`artifacts/palette-curve/report.md`](artifacts/palette-curve/report.md)。
