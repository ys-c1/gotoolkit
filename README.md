# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `cmd/tune/` 抖动强度 × 量化步长联合调参实验台（详见 `artifacts/joint_tuning.md`）
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

量化参数（默认零值为恒等映射，产物逐字节不变）：

- `--buckets N`：量化桶数（重建电平数），`0`/`256` 不量化，否则取 `[2,256]`
- `--dither S`：Floyd–Steinberg 误差扩散强度 `S∈[0,1]`，`0` 关闭抖动

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
./build/gtk encode --format pgm --in a.pgm --out q.pgm --buckets 8 --dither 0.5
go run ./cmd/tune   # 联合调参：闭式解 + 两级网格搜索 + 3×3 误差曲面
```
