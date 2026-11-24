# 行序 / 字节序 / 行对齐：八种组合与映射表

三个排布标志互相正交，每个取二值，共 2×2×2 = 8 种组合。三个都**不设**时
（`Options{}` 零值）与改动前的旧编码逐字节相同，这是兼容性的硬约束。

| 标志 | 取值 | 作用 |
| --- | --- | --- |
| `RowOrder` | `down` / `up` | 存储槽 0 放逻辑第 0 行（down）还是逻辑末行（up） |
| `ByteOrder` | `le` / `be` | 格式头里多字节整数的落字节顺序 |
| `RowAlign` | `1` / `4` | 每行补零到 1 字节（紧凑）或 4 字节 |

`api.Options` 与 CLI（`--row-order` / `--byte-order` / `--row-align`）都能设置。
空串 / `0` 表示“沿用该格式旧文件的行为”，因此：

| 格式 | 旧文件隐含的排布 |
| --- | --- |
| pgm | down / le / 1 |
| bmp | **up** / le / 4 |
| rle | down / le / 1 |

零值 `Options` 解出的图必须与旧文件一致；显式给出三个标志时，八种组合解出的图
彼此逐像素相同，也与旧文件一致。

## 各格式里三个标志的可观察性

| 格式 | RowOrder | ByteOrder | RowAlign |
| --- | --- | --- | --- |
| pgm | 行存储顺序改变 | **不生效**：PGM 头是 ASCII 十进制，没有多字节整数 | 像素区行尾补零 |
| bmp | 行顺序 + `bfHeight` 符号（up→正，down→负） | 54 字节头的所有多字节整数 | 行跨度，及 `bfSize` / `biSizeImage` |
| rle | 游程展开的存储顺序 | GTR1 头的宽、高 | 解压后像素流的每行跨度 |

`ByteOrder` 在 pgm 上不生效是格式本身的性质，不是漏实现。`api.Describe`
会把它列进 `InertFlags()`，映射表也会逐张标注“未生效标志”。
若某画幅行宽本来就是 4 的倍数，`RowAlign` 同样没有作用对象，也会被标注。

## 映射表

映射表的权威版本由 `go run ./cmd/layoutcheck` 生成到
`artifacts/layoutcheck/tables.txt`：三格式 × 八组合 = 24 张，每张逐行列出
`存储槽 → 逻辑行、偏移、有效字节数、补位数、该行字节`。

这里给出同一份 5×5 样本（像素 `10,14,18,…` 步长 4）的缩略表，
只列每行首字节与偏移，完整字节见生成物：

样本：5×5，像素 P(x,y) = 10 + 4·(5y+x)；

| 组合 | RowOrder | ByteOrder | RowAlign | pgm 行跨度 | bmp 行跨度 | rle 行跨度 |
| --- | --- | --- | --- | --- | --- | --- |
| C0 | down | le | 1 | 5 | 15 | 5 |
| C1 | down | le | 4 | 8 | 16 | 8 |
| C2 | down | be | 1 | 5 | 15 | 5 |
| C3 | down | be | 4 | 8 | 16 | 8 |
| C4 | up | le | 1 | 5 | 15 | 5 |
| C5 | up | le | 4 | 8 | 16 | 8 |
| C6 | up | be | 1 | 5 | 15 | 5 |
| C7 | up | be | 4 | 8 | 16 | 8 |

逐行落点（三格式通用；`slot` 为存储槽，`log` 为逻辑行）：

| 组合 | slot 0 | slot 1 | slot 2 | slot 3 | slot 4 |
| --- | --- | --- | --- | --- | --- |
| C0 / C1 / C2 / C3 | log 0 | log 1 | log 2 | log 3 | log 4 |
| C4 / C5 / C6 / C7 | log 4 | log 3 | log 2 | log 1 | log 0 |

跨格式的字节差异只在头部：C0/C4 的 pgm 与 C2/C6 的 pgm 头完全相同（ASCII），
而 bmp、rle 的头随 `ByteOrder` 改变：

| 组合 | rle 头 8 字节 | bmp 头关键字段 |
| --- | --- | --- |
| C0 / C4 / C1 / C5 | `47 54 52 31 05 00 05 00` | `bfSize=0x81`(le)…`biHeight=+5`(up) / `-5`(down) |
| C2 / C6 / C3 / C7 | `47 54 52 31 00 05 00 05` | `bfSize=0x00000081`(be)…`biHeight` 同样按 be 落字节 |

## 验证

```sh
go run ./cmd/mksamples -out testdata/samples   # 生成三份样本（显式拼字节，不调用 codec）
go run ./cmd/layoutcheck -samples testdata/samples -out artifacts/layoutcheck
```

`layoutcheck` 做四件事，任何一项不过都以非零码退出：

1. 生成并写出 24 张映射表（三格式 × 八组合），逐行列出。
2. **旧文件组单独验**：`testdata/golden/*.bin` 与 `testdata/samples/sampleN.<旧扩展名>`
   在**不带任何标志**时往返逐字节一致，且解出的图等于 `.src.pgm`。
3. 三份样本 × 八组合 = 24 个结果，两种读法都覆盖、各 24 个：
   - 3a：每份 `testdata/samples/sampleN` 的每个组合都按 pgm/bmp/rle 各编解一次，
     要求同组合三格式逐像素相同，且与旧文件一致；
   - 3b：把三份 `testdata/golden` 也当作三份样本，各按自身格式（pgm/bmp/rle）
     跑八组合，共 24 个结果。
4. 故障注入：故意让某个标志写入时失效，要求 `api.Diagnose` 恰好点名那一个标志。

报告写入 `artifacts/layoutcheck/report.txt`，映射表写入
`artifacts/layoutcheck/tables.txt`。

## 不一致时如何点名标志

`api.Diagnose(body, declared, want)` 在 8 种组合里搜索能解出 `want` 的组合：

- `Diff`：所有可行组合都与声明值不符 → 该标志“没生效”，列出声明值与实际值。
- `Ambiguous`：可行组合里既有声明值又有别的值 → 本格式/本画幅下不可从字节判定
  （如 pgm 的 `ByteOrder`），不误判为没生效。
- `Matched` 为空：8 种都解不出，说明数据被破坏或根本不是同一张图。

`layoutcheck` 在任一结果不一致时直接调用它，并在失败行写出
“未生效标志: <标志> 声明 <值> 实际 <值>”。
