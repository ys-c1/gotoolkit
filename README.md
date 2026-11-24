# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `internal/batch/` 目录级批量转码：目录遍历、逐文件转码、失败隔离
- `cmd/gtk/` 命令行，`encode`、`decode` 两条单文件子命令，外加 `batch-encode`、`batch-decode`
- `cmd/genbatchfixture/` 仅用于批量端到端测试的夹具生成器
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## 批量链路

```
./build/gtk batch-encode --format bmp --in 输入目录 --out 输出目录
./build/gtk batch-decode --format bmp --in 输入目录 --out 输出目录
```

- 非递归遍历输入目录，只取普通文件：encode 收 `.pgm`，decode 收对应格式扩展名（`.pgm/.bmp/.rle`），按文件名排序逐个处理。
- 任一文件失败只记录、不中断，其余文件继续；输出文件名按目标格式替换扩展名。
- 单文件的编解码仍走 `api.Encode/Decode`，因此批量产物与单文件命令**逐字节相同**。

退出码约定：

| 码 | 含义 |
|----|------|
| 0 | 全部文件成功 |
| 1 | 有文件失败（失败隔离，其余已处理） |
| 2 | 用法/参数错误 |
| 3 | 输入目录为空（无候选文件） |
| 4 | 权限不足（无法遍历目录或读写文件） |

机读输出（stdout，制表符分隔，字段内制表/换行已替换为空格）：

```
FAIL\t<文件名>\t<原因>                              # 失败文件：文件名与原因分两列
SUMMARY\ttotal=<N>\tsucceeded=<N>\tfailed=<N>       # 汇总行：固定四字段
```

汇总恒满足 `total == succeeded + failed`，且与目录中的候选文件数相等。

## 端到端记录

```
make batch-e2e
```

跑满十二组（2 方向 × 3 格式 × 全成功/带失败），每组在 `artifacts/batch-records/gNN.record`
留一份机读记录，`index.tsv` 为汇总索引，并附空目录（码 3）与权限不足（码 4）两个错误码场景。
少一组即判定未完成。
