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

## 预测器 × 块长联合代价实验（jnt / GTR3）

候选固定为两种预测器 `none|left` 与两档块长 `B长=N`（整幅一块，当前档）、`B短=256`。
联合代价 `C(P,B)=Σ n_k·H_k(P,B)+48·⌈N/B⌉`（残差熵负载 + 每块 6 B 块头），在 2×2 候选中取最小。

```
make joint        # = go run ./cmd/jointbench：算熵/代价、求解、三组各一次计时编码，
                  #   生成 artifacts/joint_cost_report.md 与 artifacts/joint/*.gtr
```

`jnt` 为实验格式：残差逐块做 DEFLATE。**当前组合** `--format jnt --predict none --block 0`
直接回落 GTR1，在基准样本上与 `testdata/golden/rle.bin` 逐字节相同；默认 `rle/pgm/bmp` 与 golden 不变。

```
./build/gtk encode --format jnt --predict left --block 256 --in a.pgm --out a.gtr
./build/gtk decode --format jnt --in a.gtr --out b.pgm
```
