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

## 位深提升映射分析

`internal/bitdepth` 给出 6-bit → 7/8/9-bit 的约束映射、位移空档放大公式与峰偏断言；
可复算图表和报告由下面的脚本生成：

```
python3 -I analysis/bitdepth_mapping.py --out artifacts/bitdepth_mapping
```

产物见 `artifacts/bitdepth_mapping/REPORT.md`，包含三档位深差表、公式证明和并排直方图。
