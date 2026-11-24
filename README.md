# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode`、`decode` 与 `batch` 三条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
./build/gtk batch --in-dir in/ --out-dir out/ --format bmp --report report.csv
```

`batch` 依次完成批量转码、逐文件回读校验与汇总报表：每个文件转码后立刻校验，失败文件不进入报表；
报表固定三列 `文件名,字节数,校验结论`，落盘后会被读回，并同时做结构校验与内存版本比对。
失败文件打印到 stderr，其余照常处理。退出码：`3` 目录不合法（`api.ErrBadDir`），
`4` 报表写入失败（`api.ErrReportWrite`），其他错误为 `1`。
