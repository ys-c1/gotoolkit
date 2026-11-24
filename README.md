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

## 内存预算与分块解码

超大画幅直接整图 `make` 会 OOM 崩溃。解码前先做内存核算，超预算在分配前拒绝，
预算紧张时自动走按行带（band）的流式分块解码。

- `Options.MemBudget`：工作集预算（字节，含输入缓冲与固定余量）。`0` 不限，走历史原路径。
  预置两档：`api.BudgetTight`（512KiB）、`api.BudgetGenerous`（8MiB）。
- `Options.BandRows`：每块行数，`0` 按预算自动选最大可容纳块。
- `api.MaxDecodablePixels` / `api.MaxChunkedSize`：按预算反解最大可解码画幅
  （整图上限 / 宽度感知的分块上限与高度）。
- `api.DecodeTo` / `api.DecodeToPGM`：分块流式解码，逐块经**双实现逐字节比对**
  与**边界像素抽查**后才合并；分块结果与整图解码逐字节相同。
- 超预算返回包装 `api.ErrBudget` 的 `codec.BudgetError`，`Item` 指明超限项
  （输入缓冲 / 输出像素缓冲 / 分块工作集）。

内存系数（n=像素数，分块为双行带缓冲的线性保守口径）：

| 格式 | 整图峰值 | 分块线性峰值 |
|---|---|---|
| pgm | 2n | 3n |
| bmp | 4n | 5n |
| rle | 3n（最坏编码） | 4n |

```
./build/gtk decode --format pgm --in big.bmp --out out.pgm --mem-budget tight
./build/gtk decode --format pgm --in big.bmp --out out.pgm --mem-budget 1048576 --band-rows 64
go run ./artifacts/report   # 两档预算的反解上限与实测峰值报告
```
