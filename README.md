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

## 位深量化与误差熵

`internal/quant/` 实现截断位深量化（步长 Δ=2^(b−1)，误差即被丢弃的低 b−1 位），
经 `Options.Depth`（档位 b=2..8，0 不量化）暴露到 `api` 与 CLI 的 `--depth`。
推导（熵随位深线性、斜率为 1，均匀分布为取等条件）与三档实验见
[QUANT_ENTROPY.md](QUANT_ENTROPY.md)。

```
make quantexp   # 三档位深 b=2,3,4 的实测熵 vs 理论值 b-1，产物在 artifacts/
python3 -I scripts/verify_entropy.py artifacts   # 独立交叉验证
./build/gtk encode --format pgm --in a.pgm --out q.pgm --depth 3
```
