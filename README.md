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

## 亮度守恒的三通道展开（仅 bmp，默认关闭）

`--luma-expand`（库侧 `Options.LumaExpand`）把灰度展开成 R/G/B 三通道，
系数由亮度守恒 `Σ w_c·k_c = 1` 与最小范数条件反解：

```
k_c = Luma_c / Σ_j Luma_j²,   Luma=(299,587,114),  Σ Luma_j²=446966
kR=299000/446966  kG=587000/446966  kB=114000/446966
```

- 不传该开关时三种格式的字节与 `testdata/golden` 逐字节一致，原灰度路径不变；
- 展开后 BMP 头长度仍是 54，`biReserved` 写 `"LUM1"`，解码自动识别，总长不变；
- 往返误差按像素给上界 `|e| ≤ 1/2 + Σ w_c·max(1/2, d_c)`；三档灰度上界为
  **1 / 1 / 47** 级，47 来自 G 通道在 `g>194` 时撞 255 截断；
- 超限（`|e|>1`）逐像素列在报告里，并说明成因。

```
./build/gtk encode --format bmp --luma-expand --in a.pgm --out a.bmp
go run ./cmd/lumareport -out artifacts/luma   # 系数、守恒、三档、熵、头部开销
```

推导、六张实测表与复算步骤见 [docs/expand.md](docs/expand.md)。
