# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `internal/subband/` 子带编码：多级 Haar DWT、反向注水/平分比特分配、均匀量化与 GTS1 码流
- `cmd/gtk/` 命令行，`encode`、`decode` 与 `subband` 三条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## 子带编码与反向注水

`subband` 格式把图像做 `--levels` 级二维 Haar 分解（默认 2 级，7 个子带），
按各子带方差 σ² 分配系数比特。

- 反向注水（默认）：最小化加权失真 `Σ w_i σ_i²4^-b_i`、约束 `Σ n_i b_i=B`。
  KKT 给出活动带共同失真水位 θ，`b_i*=½log₂(σ_i²/θ)`，σ²≤θ 的带不活动（零比特、均值重构）；
  活动集迭代剔除越界带并闭式重解水位，连续解就近取整后按边际失真削峰/填谷，
  超预算即截断并记录截断比特数。
- 平分（`--equal`）：每带统一 `floor(B/N)` 比特，作为可切回的基线；两条路径补齐到同一体积对比。
- 码流 GTS1：固定头 + 每带边信息（min/mean/range/bits）+ 紧凑系数位，零比特带不写系数。

```
# 三档预算（1/2/3 bpp）的逐带分配表、注水迭代残差、加权 MSE/PSNR 与误差-比特验证
./build/gtk subband --in a.pgm --levels 2 --bpp 1,2,3 --out artifacts/subband

# 实际编解码；--equal 切回平分路径
./build/gtk encode --format subband --in a.pgm --out a.gts --budget 8192
./build/gtk decode --format subband --in a.gts --out b.pgm
```

`gtk subband` 不带 `--in` 时使用内置确定性 64×64 合成图（径向渐变+方块+弱噪声）。
误差与比特的精确关系是指数律 `D/σ²≈c·4^-b`（log 后线性，斜率理论值 -ln4），
报告同时拟合字面反比 `D/σ²~1/b` 与 Spearman 秩相关作为对照。

