# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `internal/hybrid/` 按 16×16 块分区、平坦/纹理两条路径的混合编码与阈值分析
- `internal/testimg/` 三张确定性 320×320 测试图（平坦区与纹理区混合）
- `cmd/gtk/` 命令行，`encode`、`decode`、`analyze` 三条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `testdata/images/` 由 `tools/genimages` 生成的测试 PGM
- `tools/charts/` 从分析产物重画图表的脚本
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
./build/gtk encode --format hybrid --in a.pgm --out a.gth   # 按区切换，体积不劣于单一格式
./build/gtk analyze --in a.pgm --mask mask.pgm --tsv sweep.tsv --json summary.json
```

混合编码的阈值推导、唯一极小点证明与三张图的分区/失真/体积结果见
[`artifacts/hybrid/REPORT.md`](artifacts/hybrid/REPORT.md)；`make analyze` 可逐字节重建全部产物。
