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

## 灰度映射（LUT）

8/10/12 位深的 gamma 2.2 灰度查找表：逐点取整会在暗部产生倒挂，用累积最大值
强制单调并对平台段做整数插值铺开。证明与误差界见 [`docs/graymap-proof.md`](docs/graymap-proof.md)。

```
go run ./cmd/gtk graymap --outdir artifacts      # 生成 LUT 与倒挂坐标 CSV
python3 scripts/plot_graymap.py artifacts        # 绘制映射曲线图 PNG
python3 scripts/verify_graymap.py artifacts      # 只读 CSV 的独立复核
```

- `internal/graymap/` 实现（含定理条件与端点/平台性质的单元测试）
- `artifacts/lut_<bits>bit.csv`、`artifacts/inversions_<bits>bit.csv`、`artifacts/curve_<bits>bit.png`
