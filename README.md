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

## 低位深三处联动

`--bits` 是唯一共享参数，驱动量化步长、抖动误差基准与调色板/索引位宽。

```
./build/gtk encode --format bmp --in a.pgm --out a.bmp --bits 4 --force on
./build/gtk dump   --in a.pgm --bits 2 --target-err 43
make matrix                          # 18 组合 × 两档 -> artifacts/lowbit/
```

错误码：`E_LOWBIT_BITS`（位深越界）、`E_LOWBIT_KERNEL_SUM`（核权重和为零），CLI 退出码 3。
详见 `artifacts/lowbit/REPORT.md`。
