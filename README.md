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

## 率失真编码

`Options.EnableRD` 打开后做三件事：

1. **量化步长搜索**：先按目标每像素比特反解初始步长（高率模型 `Q≈√(12σ²)·2^(−bpp/2)`），再在 `[1,255]` 上逐档量化、用真实编码器计字节，选与目标比特差最小的步长；
2. **预测残差编码**：左邻 DPCM，残差在预测域上计算，闭环重建避免漂移；
3. **目标体积约束**：以最终落地字节对账。目标不可达不报错，`RDReport.Reachable=false` 并给出最接近步长与差距（bits / bpp / 比例）。

公共参数字段（`api.Options`）：`EnableRD`、`TargetBPP`、`Step`（0=自动搜索）。
错误码：`ErrBadTarget`（目标非正/NaN/Inf）、`ErrZeroStep`（反解步长为零，目标高到最细步长也够不着）。
关闭时三格式输出与 `testdata/golden/` 逐字节一致。

```
# 单图编码
./build/gtk encode --format rle --rd --bpp 2.0 --in a.pgm --out a.rle
# 十二组对账（3 格式 × 4 档 bpp）+ 一档极端值，实际/目标并列成表、超阈值红色标注，每档一行 JSONL 原始记录
./build/gtk rd --in a.pgm --record artifacts/rd-records.jsonl [--threshold 0.1] [--no-color]
```

