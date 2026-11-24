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

## 量化器 × 二阶反馈联合设计

低频噪声偏多的修复在 `internal/quantfb/`（推导与两行有界性证明见
[`internal/quantfb/DESIGN.md`](internal/quantfb/DESIGN.md)）：
量化步长与反馈系数交替求解到不动点
`q*=10，c1=−1.8226665269，c2=+0.8226665269`，输出误差界 `B*=18.2267≤18.5`。
A 组 Floyd–Steinberg 原核 `7/3/5/1` 原样保留在
`internal/quantfb/kernel.go` 当底稿（有测试锁值，勿删）。

```
go test ./internal/quantfb/      # 系数/不动点/误差界/指标回归
go run ./cmd/quantreport          # 生成 artifacts/quantfb/ 报告、残差 CSV、A/B/C PGM
```

对外入口：`api.FixedPointFeedback()` 与 `api.QuantizeJoint(img)`。
实测主试片低频占比 A `0.0785` → C `0.0040`（降约 19.8 倍），
量化 RMSE `12.23 → 2.90`（不升反降），全图 `max|d|=18 ≤ B*`。
