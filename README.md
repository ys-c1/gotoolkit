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

## 块边界去块滤波（最优强度）

块 DC 量化压缩会在格子边产生台阶。按边界量化误差分布推出闭式最优强度
`α* = Vn/(Vn+Vs)`，并在三档块长 4/8/16 上与固定 `α=0.5` 对照：

```
make study
```

完整推导、代入数值、并列指标表与“滤波关闭产物不变”的验证见
[`docs/deblock-optimal-strength.md`](docs/deblock-optimal-strength.md)，
产物在 `artifacts/deblock/`。解码时可用
`--block B --deblock-alpha A` 开启滤波；零值（默认）关闭，产物逐字节不变。
