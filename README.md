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

## 有序抖动

`internal/dither/` 提供三种 8×8（等级 0..63）阈值矩阵，经 `api.Options.Dither`
或 `api.Dither` 使用；默认不抖动，原有编解码行为不变。

- `bayer`：经典递归 Bayer（原矩阵，保留可选）；
- `void-cluster`：Ulichney 蓝噪声矩阵，固定 LCG 种子 20261125，σ=1.5；
- `spectral`：以 void-cluster 为初值、按矩阵傅里叶低频泄漏贪心交换得到，
  低频泄漏比 bayer 降约三个数量级，规则纹对比度（PAR、各向异性）显著下降，
  灰场量化 MSE 与 bayer 严格相等。

设计推导、傅里叶系数取法、矩阵完整内容与改造前后对比图见
[`docs/dither.md`](docs/dither.md)，numpy 独立原型在 `analysis/`。

```
./build/gtk dither-report                       # 三矩阵低频能量与误差并列
./build/gtk encode --format pgm --in a.pgm --out b.pgm --dither spectral
```
