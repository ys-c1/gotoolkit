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

## 误差扩散反馈核

`internal/dither/` 提供带误差反馈回路的光栅误差扩散：现成 Floyd–Steinberg 核
保留可选，另有三个按白噪声目标谱反解的 4 阶反馈核（低频噪声占比降为原核的
1/43–1/59，MSE 同时下降）。设计推导、逐点幅度响应与系数明细见
[`analysis/DESIGN.md`](analysis/DESIGN.md)，复现脚本在 `analysis/`。
