# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 格式实现，外部不可引用：PGM/BMP、GTR1 游程（`rle`）、
  GTR2 块级自适应游程（`rle2`，设计见 `docs/rle2.md`）
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令；`cmd/rle2bench/` 出
  rle2 三档体积与熵下界对照
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
./build/gtk encode --format rle2 --tier 1 --in a.pgm --out a.gtr2
go run ./cmd/rle2bench
```
