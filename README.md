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

## 排布标志（行序 / 字节序 / 行对齐）

`api.Options` 与 CLI 都支持三个正交标志，八种组合解出的图必须逐像素相同，
且与不带标志的旧文件一致（零值 `Options` 与旧编码逐字节相同）：

| 标志 | CLI | 取值 | 默认（旧行为） |
| --- | --- | --- | --- |
| `RowOrder` | `--row-order` | `down` / `up` | pgm、rle 为 `down`；bmp 为 `up` |
| `ByteOrder` | `--byte-order` | `le` / `be` | `le`（pgm 头是 ASCII，此标志对 pgm 不生效） |
| `RowAlign` | `--row-align` | `1` / `4` | pgm、rle 为 1；bmp 为 4 |

映射表、逐行落点、不一致时如何点名未生效的标志，见 `docs/layout.md`。
验证：`go run ./cmd/layoutcheck -samples testdata/samples`。

```
./build/gtk encode --format bmp --row-order down --byte-order be --row-align 4 --in a.pgm --out a.bmp
```

