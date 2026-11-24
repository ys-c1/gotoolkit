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

## 按目标体积反解质量参数（BLK 分块位深）

“体积要么太大要么糊、参数只能试”的解法：新增一个体积关于质量**严格单调**的
`blk`（魔数 `BLK1`）分块格式，先把单调关系写成精确式子，再用二分反解质量参数，
最后用**真实编码落地字节**对账。

### 1. 体积随参数的单调关系（先推导）

质量参数 `Q∈[1,100]` 唯一决定块内打包位深

```
d(Q) = ceil(8Q / 100) ∈ [1,8]          # Q↑ ⇒ d 单调不减
```

图像按边长 `B` 分块，块数 `N = ceil(W/B)·ceil(H/B)`，第 j 块像素数 `k_j`。
每块写 `min,max` 两字节 + `ceil(k_j·d/8)` 字节定长打包索引，因此整条流长度是
**精确**的（不做熵编码，没有内容相关的抖动）：

```
S(Q;B) = 10 + 2N + Σ_j ceil(k_j · d(Q) / 8)
```

`d(Q)` 单调不减 ⇒ `S(Q)` 关于 `Q` **单调不减**，这是二分成立的依据。
宽高都能被 `B` 整除时 `k_j=B²`，化简为

```
S(Q;B) = 10 + (W/B)(H/B) · (2 + B²·d(Q)/8)
```

256×256、`B=8` 时：`N=1024`、`S(Q)=2058 + 8192·d(Q)`（字节），每加 1 位深恰好多 `8192 B`。
该式由 `TestBLKSizeEqualsActualLength` 逐档对照真实 `len(body)` 锁死。

### 2. 二分求解 + 区间外给最接近值

`target.Solve`（公共入口 `api.SolveQuality`）：

1. **二分**最小的使 `S(Q) ≥ 区间下界` 的 `Q`（lower_bound），保留每次
   `(lo, mid, hi, 实测字节)`；100 档最多 7 次试探。
2. **回代实测**：若该 `Q` 的真实字节 `∈ [MinBytes,MaxBytes]`，命中。
3. **区间外不报错**：在整个 `Q∈[1,100]` 上选到区间距离最小的**最接近参数**，
   返回 `ClosestQuality/ClosestBytes/GapBytes/GapPercent/Side`，说明差多少、偏哪一侧。
   同一 size 平台上平局取最小 `Q`（达到该体积的最省参数，与 lower_bound 一致）。

### 3. 分块尺寸与预算的关系（式子）

给定总预算 `T` 与块边长 `B`，每块可花 `b_blk = (T−10)/N` 字节，扣掉 2 字节 `min/max`：

```
d* = floor( 8·(b_blk − 2) / B² )
```

整除网格代入 `N=WH/B²`：

```
d* = 8(T−10)/(W·H) − 16/B²
```

第一项是总预算对应的每像素位数，第二项 `16/B²` 是每块 2 字节头摊到 `B²` 像素上的
每像素位开销——**块越大，块头开销越小**。反向，给定每块预算 `b_blk` 与目标位深 `d`：

```
B ≤ floor( sqrt( 8·(b_blk−2)/d ) )
```

对应 `api.BLKBudgetDepth` / `api.BLKBlockSizeForBudget` 与 `TestBudgetDepthGuaranteesFit`。

块头开销项 `16/B²` 的数值对照（256×256）：

| B | 块数 N | 块头开销 16/B² | d=1 实测体积 |
|---|---|---|---|
| 4 | 4096 | 1.000 bpp | 10 + 4096·(2+2) = 16394 B |
| 8 | 1024 | 0.250 bpp | 10 + 1024·(2+8) = 10250 B |
| 16 | 256 | 0.0625 bpp | 10 + 256·(2+32) = 8714 B |

同一预算 `T=18442 B`、`B=8` 代入整除式可直接验证：
`d* = 8·(18442−10)/65536 − 16/64 = 2.25 − 0.25 = 2`，正是二分求出的位深。

### 4. 四档目标：区间与实测逐档对照（真实运行值）

四档用目标每像素比特带描述（随画幅缩放），换算成字节闭区间。对内置确定性
256×256 图、`B=8` 实跑 `./build/gtk targets`（完整过程见
[`artifacts/target-report.txt`](artifacts/target-report.txt) 与
`artifacts/target-records.jsonl`）：

| 档 | bpp 带 | 目标区间 (B) | 解出 Q / 位深 | **实测体积 (B)** | 实测 bpp | 判定 |
|---|---|---|---|---|---|---|
| T1 极紧 | 1.00–1.60 | [8192, 13107] | Q=1, d=1 | **10250** | 1.251 | ✓ 落入 |
| T2 紧凑 | 2.00–2.70 | [16384, 22118] | Q=13, d=2 | **18442** | 2.251 | ✓ 落入 |
| T3 均衡 | 3.50–4.60 | [28672, 37683] | Q=38, d=4 | **34826** | 4.251 | ✓ 落入 |
| T4 高画质 | 5.40–7.20 | [44237, 58982] | Q=63, d=6 | **51210** | 6.251 | ✓ 落入 |

每档都有一行把**解出的参数与实测体积放在同一行**，并附 PSNR 交代“糊不糊”：

```
★ 第2档[紧凑] 解出参数 Q=13（位深 d=2） | 实测体积=18442 B | 目标区间 [16384,22118] B | 落入区间 | PSNR=35.47 dB
```

二分过程与每次实测体积并列输出，例如 T2：

```
#1 窗口[  1,100] mid=50 d=4 实测= 34826 B  S≥下界 → hi=mid-1
#2 窗口[  1, 49] mid=25 d=2 实测= 18442 B  S≥下界 → hi=mid-1
#3 窗口[  1, 24] mid=12 d=1 实测= 10250 B  S<下界 → lo=mid+1
...
#6 窗口[ 13, 14] mid=13 d=2 实测= 18442 B  S≥下界 → hi=mid-1
```

**区间外兜底（实测）**：把窄区间 `[21172,23904] B` 故意放进 d=2(18442) 与 d=3(26634)
的体积空档，区间不可达，命令如实给出最接近值与差距：

```
窄区间 [21172,23904] B；二分 lower_bound Q=26(d3) 自身体积=26634 B，越过上限 2730 B；
→ 最接近参数 Q=13(d2)，实测体积 18442 B，位于区间下界之下（体积偏小），差距 2730 B（12.89%）。
```

### 5. 分块解码的中间缓冲如何复用

`codec.BlockDecoder`（`internal/codec/blk.go`）在 `NewBlockDecoder` 时**只分配一次**
容量 `B·B` 的 `buf []uint8`。每次 `Next()` 把当前块（边块可能更小）重建进
`buf[:k]` 并返回这个别名切片；调用方必须在下一次 `Next()` 前把像素拷走——
`DecodeBLK` 的做法是立即按行 `copy` 进整图 `Pix`。随后同一块底层数组被下一块覆写复用，
解 N 个块只有一次 `B²` 缓冲分配，没有每块一次的分配。
`TestBlockDecoderReusesBuffer` 用 `reflect.ValueOf(...).Pointer()` 校验各块返回切片
始终别名到同一底层数组、容量恒为 `B²`，且块数与解码结果正确。

### 6. 用法与公共字段

```
make targets                 # = gtk targets，四档对账 + 空档探针，写 artifacts/

# 四档对账（可用任意外部 PGM；区间按其像素数换算）
./build/gtk targets [--in a.pgm] [--block-size 8] \
    [--record artifacts/target-records.jsonl] [--outdir artifacts] [--no-color]

# 单图按区间反解：解出的 Q 与实测字节在同一行打印，并落盘该 Q 的 BLK1
./build/gtk encode --format blk --in a.pgm --out a.blk \
    --target-min 16000 --target-max 24000
./build/gtk encode --format blk --in a.pgm --out a.blk --target 30000   # 点目标 [T,T]
./build/gtk encode --format blk --quality 60 --block-size 8 --in a.pgm --out q.blk
./build/gtk decode --format blk --in a.blk --out back.pgm
```

`api.Options` 新增公共字段：`Quality`（1..100，0=最高/d=8）、`BlockSize`（0=默认 8）、
`TargetBytes`、`TargetMinBytes`、`TargetMaxBytes`；新增 `api.SolveQuality` /
`api.QualityReport`、`api.BLKPredictedSize` / `BLKDepthSize` / `BLKBudgetDepth` /
`BLKBlockSizeForBudget`。

### 7. 向后兼容：不指定目标时与现在完全一致

不设置任何 `--target*` 时，`encode` 走原来的直接编码路径；`pgm/bmp/rle` 三个编码器
一字节未改，`Options` 新字段对它们完全不生效，`testdata/golden/` 基准与
`TestGoldenBytes` 逐字节通过。只有显式使用 `--target*` / `SolveQuality` 或 `format=blk`
才进入新能力。`make test` 含单调性、公式=实测、二分命中/各侧兜底、缓冲复用、
四档 CLI 对账与黄金字节等测试。
