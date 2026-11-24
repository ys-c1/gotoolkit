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

## 索引重排（rle 专用）

`--reorder` 把索引序列按值重排使游程数降到理论最优（不同索引个数 k），
输出 GTR2 流（内含差分变长置换），解码端按魔数自动识别并逆置换，
解码结果与原图逐像素一致：

```
./build/gtk encode --format rle --reorder --in a.pgm --out a.rle2
./build/gtk decode --format rle --in a.rle2 --out b.pgm   # 自动识别 GTR2
```

理论推导（排序问题 ≡ 路径图着色、着色下界反解最优值、近似算法误差界）
见 `docs/palette-reorder.md`；三张图的对照实验由 `make lab` 生成，
产物在 `artifacts/reorder/`（游程数按整图统计）。

