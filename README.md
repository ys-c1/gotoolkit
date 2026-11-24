# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `cmd/predictbench/` 预测编码的实测脚本（生成临时图像并打印对比表）
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
./build/gtk encode --format rle --predict-order 4 --in a.pgm --out a.rle
go run ./cmd/predictbench
```

## RLE 预测编码

`api.Options.PredictOrder` 是公共配置里的唯一入口：0（默认）关闭预测，输出
与原 GTR1 逐字节一致；`1..api.MaxPredictOrder`（上限 4）表示在候选集
`{不预测} ∪ {Kx+Ky ≤ N}` 里按残差一阶熵最小自动选阶（Kx 水平、Ky 竖直）。
选阶依据、熵的计算方式与统计区间、选中候选的残差分布都由 `gtk encode`
打印；`gtk decode` 报出流内记录的候选。实现与格式说明见
`internal/codec/predict.go`。
