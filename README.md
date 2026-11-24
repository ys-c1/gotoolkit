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
./build/gtk scale --in a.pgm --out half.pgm --ratio 0.5 --space encoded
./build/gtk gamma-lab --gamma 2.2 --tolerance 1
```

伽马/缩放导致的暗边偏差、反解补偿公式与两档画幅复算见
[`docs/gamma-scale.md`](docs/gamma-scale.md)。`--space encoded`
始终保留直接在编码值上缩放的原路径。
