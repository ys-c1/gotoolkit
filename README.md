# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 四个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `cmd/blkexp/` 块长实验：三档块长的方差/重建误差/边界差实测
- `testdata/golden/` 三种无损格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准
- `docs/blk-blocklen.md` 块压缩重建误差表达式、最优块长推导与实测对照

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm

# 块均值有损压缩（GTB1）：--block 指定 L×L 块边长，默认 8，须同时整除宽高
./build/gtk encode --format blk --block 8 --in a.pgm --out a.gtb
./build/gtk decode --format blk --in a.gtb --out b.pgm

# 块长实验（输出 artifacts/blkexp/ 下的表与 PGM）
go run ./cmd/blkexp
```

## blk 格式

流布局：`GTB1` + 宽(2 LE) + 高(2 LE) + 块长 L(1) + 各 L×L 块均值
（uint8，四舍五入）。每块存 1 字节，解码整块回填。L 必须同时整除宽高，
否则编解码均报错；L=1 时无损。误差推导与三档实测见 `docs/blk-blocklen.md`。

