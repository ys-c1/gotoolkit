# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `crosscheck/` 命令行与库调用的逐字节对拍测试（九组矩阵 + 分段直通 + 差异图）
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## 命令行 vs 库调用对拍

同一份参数分别走两条路径，产物逐字节比较：

- 命令行：`gtk encode --format F --gain G --in in.pgm --out cli.bin`
- 库调用：`api.PipeStages(raw, "encode", F, G)`（内部即 `api.Decode`/`api.Encode`）

两条路径都经由 `api.PipeStages`（见 `api/stages.go`），管线显式拆成四个可观测环节：

| 环节 | 名称 | encode 子命令 | decode 子命令 |
| --- | --- | --- | --- |
| 1 | `s1_pgm_decode` | 输入 PGM 解码出的原始灰度图 | 输入格式解码出的原始灰度图（不带增益） |
| 2 | `s2_gain` | 正向增益后的灰度图 | 反向增益后的灰度图（`applyGain(-gain)`） |
| 3 | `s3_format_enc` | 目标格式编码出的字节流 | 回写成 PGM 的字节流 |
| 4 | `s4_file_bytes` | 最终写盘字节（与环节3相同） | 最终写盘字节（与环节3相同） |

gain=0 时环节 1、2 的像素相同；gain 非 0 时两者不同，`api/stages_test.go` 对该性质有断言。

CLI 只有在设置环境变量 `GTK_DUMP_DIR` 时才把四个环节落盘；不设置时行为零变化。
对拍测试（`crosscheck/`）逐环节比较库侧产物与 CLI 转储，最终产物不一致时，
第一个分叉的环节就是差异定位结果，并在 `artifacts/diffmaps/` 生成差异图
（SVG 热力图 + 文本偏移清单，该目录不入库）。

### 九组结论（3 格式 × 3 增益）

增益取 -50 / 0 / +50，格式取 pgm / bmp / rle。以下结论由 `go test ./crosscheck/`
逐组打印并断言；encode 与 decode 两条子命令各跑一遍同样的九组。

| 组号 | 格式 | 增益 | 一致 | 差异字节数 | 首个分叉环节 |
| --- | --- | --- | --- | --- | --- |
| 1 | pgm | -50 | 是 | 0 | — |
| 2 | pgm | 0 | 是 | 0 | — |
| 3 | pgm | +50 | 是 | 0 | — |
| 4 | bmp | -50 | 是 | 0 | — |
| 5 | bmp | 0 | 是 | 0 | — |
| 6 | bmp | +50 | 是 | 0 | — |
| 7 | rle | -50 | 是 | 0 | — |
| 8 | rle | 0 | 是 | 0 | — |
| 9 | rle | +50 | 是 | 0 | — |

九组全部一致，无分叉环节。对拍输入刻意覆盖边界：BMP 非 4 对齐宽度的行填充、
RLE 跨 255 的长游程截断、0/255 极值与增益截断。`TestLocalizationWithInjectedFault`
用故障注入自检：人为篡改某环节字节后，分段直通必须定位到该环节且差异图必须包含该偏移。

### 退出码与库返回值的对应关系

库以 `(值, error)` 返回；CLI 把结果映射成进程退出码（由 `TestExitCodeMapping` 逐行验证）：

| 场景 | 库返回 | CLI 退出码 |
| --- | --- | --- |
| 成功，产物写盘 | `nil` error | `0` |
| flag 解析失败 / 缺 `--in`/`--out` | 库未被调用（用法层拦截） | `2` |
| 未知格式 | 包裹 `api.ErrUnknownFormat` 的 error | `1` |
| 增益越界 | 包裹 `api.ErrBadOption` 的 error | `1` |
| 空/截断输入 | 包裹 `api.ErrTruncated` 的 error | `1` |
| 画幅非法 | 包裹 `api.ErrBadSize` 的 error | `1` |
| 输入文件不存在等读盘错误 | 库调用前的 IO 错误，无对应库 error | `1` |
