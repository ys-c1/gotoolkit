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

## 误差扩散抖动

参数在 `api.Options`（同义名 `api.Parameters`）：

- `DitherStrength float64`：扩散强度，缺省 `0`＝关闭，此时输出与三份基准逐字节一致；
- `DitherKernel DitherKernel`：扩散核，内置 `floyd-steinberg`（归一）与 `atkinson`（不归一，带理由）；
- `DitherLevels int`：目标色阶数，缺省 `0` 表示 8；显式给值须在 `[2,256]`。

```
./build/gtk encode --format bmp --in a.pgm --out a.bmp \
    --dither-strength 1 --dither-kernel floyd-steinberg
./build/gtk kernels                               # 归一后的权重表
go run ./cmd/ditherreport -out artifacts/dither   # 体积与相邻色阶间距方差
```

数据与边界核对见 `artifacts/dither/report.md`。
