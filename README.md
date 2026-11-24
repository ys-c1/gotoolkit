# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `cmd/bench/` 熵/体积度量与无损往返校验，`make bench`
- `testdata/golden/` 三种老格式的基准字节，由 `ref/` 生成，**不许改**
- `testdata/samples/`、`testdata/decoded/` bench 生成的样本与解码图
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## 格式

| format | 说明 |
|---|---|
| `pgm` | P5 灰度，逐字节存储 |
| `bmp` | 24 位灰度 BMP，三通道同值，4 字节行对齐 |
| `rle` | `GTR1` 游程：(计数,值) 序列；长渐变会膨胀到约 16 bit/px |
| `dict` | `GTD1` 无损字典编码，见下 |

### GTD1（dict）

针对 RLE 在长渐变上膨胀的问题，GTD1 做一阶差分（模 256）后用规范化
Huffman 编码，布局为小端：

```
"GTD1" | 宽u16 | 高u16 | D u8 | 字典符号[D] | 码长[D] | 载荷比特数u32 | bit流(MSB优先)
```

- 频次 ≥2 的差分符号进字典（`0xFF` 保留为转义符 ESC，不作普通成员）；
  其余符号走转义：ESC 码字 + 8 bit 字面量。
- 字典最多 255 个普通成员 + ESC；槽位不足时频次最低者降级为转义。
- 解码严格校验魔数、画幅、Kraft 不等式、比特数与像素数，截断即报错。
- 编码确定性（同输入逐字节同输出），解码图与原图逐像素一致。

## 度量

`make bench`（或 `go run ./cmd/bench`）对三份 256×256 样本（长渐变、
大块+孤立点、伪随机噪声）输出：

- 按符号出现次数计算的信息熵（比特/像素）与符号种类数；
- GTD1 体积、体积与熵下界的差值（字节 + 占下界百分比）；
- 文件头/字典/bit 流分项开销，字典码字与转义开销分列；
- 解码 PGM 落盘到 `testdata/decoded/`，可直接 `cmp` 逐像素比对；
- 报告写 `artifacts/report.md` 与 `artifacts/report.json`。

零阶符号熵是逐符号无记忆编码的下界；GTD1 先做差分去相关，因此强相关
图像的体积可以低于该下界，报告同时给出一阶差分熵口径的差距与浪费根因。
