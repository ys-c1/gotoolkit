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

## 单调重建电平（步长序列 → 单调电平）

给定任意步长序列与“凑出来”的（可能非单调）名义电平，用两步构造得到最小二乘
最优的单调重建电平（PAVA / 保序回归），最优性按互补松弛 KKT 核验：

```
go run ./cmd/levelreport            # 三档步长的电平、逐对单调性、误差与预测
go run ./cmd/levelreport -json      # 结构化结果
go test ./internal/monolevel -v     # 证明性测试（任意步长、穷举最优、50 万组前缀引理）
```

证明与三组实测数据见 `docs/monotone-levels.md`，落盘报告见
`artifacts/monolevel/`。对外入口为 `api.MonotoneReconstruct` 与
`api.AnalyzeQuantTable`。

