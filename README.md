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

## 亮度链路顺序

三件事的先后是固定的，**不可配置**，编码与解码两侧走同一份实现（`api.applyBrightness`）：

```
输入 → [1] 线性增益 Gain → [2] 伽马 Gamma → [3] 透明掩码 Mask → 输出
```

| 顺序 | 开关 | 语义 |
|:---:|:---:|---|
| 1/3 | `--gain N` | 线性增益百分比，`[-100,100]`。`x = v + trunc(v*N/100)`，逐像素钳到 `[0,255]`，**任何像素都不溢出**；`0` 表示不调整 |
| 2/3 | `--gamma V` | 伽马目标平均亮度，`(0,255]`。系数由增益后的均值反解：`exp = ln(V/255) / ln(mean/255)`；`0` 表示不调整 |
| 3/3 | `--mask F` | 8 位 PGM 透明掩码，尺寸须与图相同。`out = round(v*M/255)`，`0` 全透明，`255` 原值；不给表示不调整 |

同一顺序也写在 `gtk encode --help` / `gtk decode --help` 里，`api.Options` 的字段注释逐项标注 `[1/3] [2/3] [3/3]`。
掩码与增益同时开启时：先增益，再伽马，最后掩码——掩码作用在增益与伽马之后的像素上，不会反过来改变伽马的均值。

### 错误码

| 退出码 | 哨兵错误 | 字段 | 触发条件 |
|---:|:---:|:---:|---|
| 2 | `ErrBadOption` / `ErrUnknownFormat` | `Format`/`Gain`/`Gamma` | 参数越界或格式未知 |
| 3 | `ErrGammaZero` | `Gamma` | 增益后均值已是 0 或 255，或目标为 255，反解不出正系数 |
| 4 | `ErrMaskRange` | `Mask` | 掩码长度不等于 `W*H` |

错误文本会带回具体字段值，例如 `伽马为零: Gamma=255，增益后均值=38.500 反解出的系数=-0`。

### 验证产物

- `artifacts/metrics.md` — 十二组（3 格式 × 4 开关组合）的四项指标：字节数、平均亮度、`maxdiff`、端点像素数
- `artifacts/order/order_gain_gamma_mask.pgm` — 正式顺序（增益→伽马→掩码）的结果
- `artifacts/order/order_mask_gamma_gain.pgm` — 对照顺序（掩码→伽马→增益）的结果

两份顺序结果由 `go test ./api -run TestOrderDifference -v` 生成，可直接 `cmp` 逐字节比较；测试同时打印两行对照数据。
`artifacts/metrics.md` 由 `go test ./api -run TestTwelveGroups -v` 生成。
