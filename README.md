# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `internal/quant/` 量化重建：中点电平与条件期望（不动点迭代）两条路径
- `cmd/gtk/` 命令行，`encode`、`decode`、`sample`、`recon` 四条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
./build/gtk sample --out sample.pgm
./build/gtk recon --in sample.pgm --bits 3,4,5
```

## 量化重建

`api.Options` 的 `Bits`/`Recon`/`ReconTol`/`ReconMaxIter` 控制解码后的重建；
`Bits=0` 时保持原有逐字节回读，重建路径完全不动原解码逻辑。

- `Recon=midpoint`：`[0,255]` 均分为 `K=2^b` 个区间，电平取区间中点
  `r_k = -0.5+(k+0.5)·256/K`。
- `Recon=expectation`：对每个区间取条件期望，用不动点迭代同时更新电平与
  最近邻边界（Lloyd 式）：
  `r_k = Σ_{x∈R_k} x·p_x / Σ_{x∈R_k} p_x`，
  收敛判据 `max_k |r_k^(t+1)-r_k^(t)| <= tol`。初值为中点电平，步长为 1；
  超过 `ReconMaxIter` 轮仍未收敛则返回 `quant.ErrNotConverged`，不静默返回。

合成样本 `gtk sample` 上按 3/4/5 位深对比（`gtk recon` 可复现）：

| 位深 | 迭代次数 | 中点 MSE | 条件期望 MSE | 降幅 |
| --- | --- | --- | --- | --- |
| 3 | 6 | 67.824 | 62.791 | 7.4% |
| 4 | 3 | 20.897 | 17.485 | 16.3% |
| 5 | 3 | 4.994 | 4.461 | 10.7% |

MSE 按取整到 uint8 后的实际重建图像统计（`quant.DistortionRounded`），
两条路径同一口径；迭代次数是条件期望路径满足
`max_k |r_k^(t+1)-r_k^(t)| <= 1e-6` 的轮数，侧重中点路径为 0。
固定测试样本 `internal/quant/fixture` 与上面命令使用的是同一份图。
