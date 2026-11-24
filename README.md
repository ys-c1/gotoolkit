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

## PGM 头结构支路

在经典 P5 管线（`P5\n{w} {h}\n255\n` + 原始像素）上接出的可选支路，
由 `Options` 上两个开关控制，仅对 `pgm` 格式生效：

| 开关 | 含义 |
| --- | --- |
| `ExtHeader` | 扩展头开关：写入位深、行序标志、校验和三个字段 |
| `FlipRows` | 行序开关：像素底朝上存储，并在头中置行序标志 |

### 两开关的四种开合

| ExtHeader | FlipRows | 头布局 | 像素行序 |
| --- | --- | --- | --- |
| 关 | 关 | 仅标准 P5 头，与 `testdata/golden/pgm.bin` **逐字节相同** | 顶朝上 |
| 开 | 关 | 标准头 + 12 字节扩展块 | 顶朝上 |
| 关 | 开 | 标准头 + `#GTKR\n` 独立标志（6 字节） | 底朝上 |
| 开 | 开 | 标准头 + 12 字节扩展块（行序标志位置 1） | 底朝上 |

### 扩展块字段排布表（固定偏移，解码端按表读取）

偏移相对扩展块首字节（标准头之后），三字段互不覆盖，块总长 12 字节：

| 字段 | 类型 | 长度(字节) | 偏移 |
| --- | --- | --- | --- |
| 魔数 | `[6]byte`，内容 `#GTKX\n`（PGM 注释形式） | 6 | 0 |
| 位深 | `uint8`，仅允许 `8`，其他值报 `ErrFieldOutOfRange` | 1 | 6 |
| 行序标志 | `uint8`，bit0=1 底朝上；bit1..7 为保留位，非零报 `ErrFlagConflict` | 1 | 7 |
| 校验和 | `uint32` 大端，像素区（按存储行序）的 CRC-32/IEEE | 4 | 8 |

### 错误码

| 哨兵错误 | 触发条件 |
| --- | --- |
| `api.ErrFieldOutOfRange` | 位深字段越界（非 8），信息含位深字段偏移 6 |
| `api.ErrFlagConflict` | 行序标志保留位非零（标志冲突），信息含标志字段偏移 7 |
| `api.ErrChecksum` | 校验和与实算不符，信息含校验和字段偏移 8 及头中/实算两个值 |
| `api.ErrTruncated` | 扩展块或像素区不完整 |

所有检出错误的信息都带字段偏移位置；解码端完全按头还原开关状态，
经 `api.Image.Meta` 返回。冲突样例各保留一个，在 `testdata/pgmx/`：
`bad_bitdepth.bin`、`bad_flags.bin`、`bad_checksum.bin`（另有
`truncated_block.bin` 截断样例与四个合法组合样例），由
`make fixtures`（`cmd/genpgmx`，独立手工拼字节）生成。
