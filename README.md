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

## 多帧差分与关键帧间隔

`api.EncodeSequence` / `api.DecodeSequence` 在既有单帧格式上增加关键帧链：
`Options.KeyframeInterval=1` 为全关键帧模式，每帧仍逐字节走现有编码器；
P 帧使用 `GTD1` 量化差分负载。累积误差上界推导、三档间隔对照和逐帧实测见
[`docs/accumulation.md`](docs/accumulation.md)。

```
make chain
# 等价于 go run ./cmd/chainbench
```

机器可读记录写入 `artifacts/chain/`：`report.json`、`frame_metrics.csv`
（逐帧）、`pixel_errors.csv`（逐帧逐像素）和 `last_frame_pixels.csv`。
