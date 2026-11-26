# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 四个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode`、`decode`、`analyze` 三条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## pred 格式与预测阶数开关

`pred`（GTP1）按行做 FLAC 风格 0..4 阶固定系数预测，残差 mod 256 存储，同一位深、无损往返。

- 理想情形加阶不增熵：H(X|Y,Z) = H(X|Y) − I(X;Z|Y) ≤ H(X|Y)
- 实际增熵的条件：**σ²(e_{k+1}) / σ²(e_k) > 1**（高斯近似 H ≈ ½·log2(2πeσ²)）
- `--auto-order` 开关：仅当方差比 ≤ 1 才加阶，首次不满足即停

```
./build/gtk encode --format pred --pred-order 3 --auto-order --in a.pgm --out a.gtp
./build/gtk analyze --in a.pgm --max-order 3   # 各阶方差/方差比/熵与开关判定
```
