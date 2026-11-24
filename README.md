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

## 五抽头约束最小二乘重建核 vs 双线性

`internal/recon/` 与 `cmd/reconreport/` 是独立的彩色重建/色差核对子系统，不改动上面的灰度 codec 与 golden。

- 五个抽头按 `[上,左,中,右,下]` 排序，两条等式约束（Σh=1、h0=h4、h1=h3）直接写进 KKT 正规方程求解，不是事后检查；
- `make recon-report`（或 `go run ./cmd/reconreport`）在同一组 6 张确定性彩色图上给出 `AᵀA`、`Aᵀy`、8×8 KKT 两端、五个抽头、逐条约束打勾，以及 CIE76 ΔE 的平均/最大对照表，产物在 `artifacts/recon/`；
- 双线性路径保留：`BilinearScale` / `BilinearRoundTrip` / `MethodBilinear`，可用
  `go run ./cmd/reconreport -method bilinear|cosited|clsk -in a.ppm -out b.ppm` 切换；
- `go test ./internal/recon` 逐条核验约束、KKT 残差、色差达标，并用 `TestSwitchBackIdentical`
  保证 `bilinear → clsk → bilinear` 切回后逐像素一致、`TestBilinearPinnedMetrics` 锁定当前双线性基线。

