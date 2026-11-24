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

## 锐化

锐化是可选的，`--sharpen` 缺省时产物与原编码逐字节一致。

```sh
./build/gtk encode --format pgm --in a.pgm --out b.pgm --sharpen
./build/gtk encode --format pgm --in a.pgm --out b.pgm --sharpen --k 7
./build/gtk encode --format pgm --in a.pgm --out b.pgm --sharpen --sharpen-gain 8
```

模型为 `I' = I + g*(I - G_k*I)`，`G_k` 是 σ=(k-1)/3 的归一化高斯。
「过冲为零」指输出不越过 `[0,255]`（不截平出白边/黑边）。反解见
`internal/sharpen`，结论与三档对比度实测表见 `docs/sharpen-overshoot.md`
（`make goldenreport` 重新生成）：

- 逐像素闭式解：`g* = min_x {(255-v)/h (h>0); v/(-h) (h<0)}`，`h=I-G_k*I`
- 阶跃边缘：`g_max(C,k) = H/(C*D_k)`，`D_k=(1-w_c)/2`，`H=min(255-hi,lo)`
