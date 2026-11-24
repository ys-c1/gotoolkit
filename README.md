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

## 量化偏置研究（误差均匀性 → 散度最优重建偏移）

额外的实验子系统，验证“量化误差服从均匀分布”这一假设、推导误差分布的偏置项，
并求解使分布到均匀分布的 W₂ 散度最小的重建电平偏移：

```
make study     # = go run ./cmd/quantstudy + python3 analysis/plot_quant_bias.py
```

- 四类确定性测试图（gradient/photo/texture/noise）× 三档位深（2/3/4 bit）
- 原始零偏移量化器保留为对照；逐档给出 JS/KL/χ²/W₂ 散度与直方图
- 验收线：整数偏移后的归一化残余偏置 `|E[e]|/Δ ≤ 0.05`，四类 × 三档全部通过
- 推导、数值表、0.05 阈值来源与图表见
  [`artifacts/quant_bias/REPORT.md`](artifacts/quant_bias/REPORT.md)
- 硬断言在 `internal/quantbias/quantbias_test.go` 与 `api/quantbias_test.go`，
  随 `make test` 一起运行

