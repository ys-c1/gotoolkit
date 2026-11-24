# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `internal/palette/` 中位切分调色板量化，仅改像素、不动文件头
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

可选的中位切分调色板量化（只改像素，不动任何格式的文件头；关闭时与基准逐字节一致）：

```
./build/gtk encode --format pgm --in a.pgm --out a.pgm --palette --buckets 8
./build/gtk encode --format pgm --in a.pgm --out a.pgm --palette --buckets 8 --variance-threshold 100
```

参数、切分维与阈值推导、MSE 上界、两档桶数实测见 [docs/palette.md](docs/palette.md)，
运行记录与核对数据在 `artifacts/palette/`。
