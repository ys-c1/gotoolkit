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

## 运动估计实验

`scripts/motion_lab.py` 生成三组已知速度的多帧灰度序列，在运动区域残差曲面
`J(dx,dy)` 上用二阶差分反解曲率，并执行曲率步长与分层范围自适应的亚像素搜索：

```
make motion       # 生成 artifacts/motion/ 下的表、图和对照图
make motion-test  # 曲率公式、取整界和残差改进检查
```

主要结果在 `artifacts/motion/motion_report.md`；整像素与亚像素对照图分别是
`artifacts/motion/comparison_integer.png` 和
`artifacts/motion/comparison_subpixel.png`。
