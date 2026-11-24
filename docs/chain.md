# 处理链：掩码 → 伽马 → 抖动

三段处理合成一条链，顺序是**固定语义**，写在这里、`api.Options` 字段注释、
`gtk encode|decode --help` 与 `api.CanonicalOrder` 里，四处一致：

```
输入 → [1] 透明掩码 MaskValues → [2] 伽马 Gamma → [3] 抖动 Dither → 输出
```

`Gain` 是链外的既有步骤，链开启时仍先于链执行；链内顺序不受 `Gain` 影响。

## 每一步的含义

| 步 | `api.Options` 字段 | 默认 | 语义 |
|:---:|:---:|---:|---|
| 1/3 | `MaskValues []int` | 空（关闭） | 编码时把等于任一掩码值的像素记进尾段（`GTMK`），像素字节本身不改；解码后 `Image.Alpha` 逐像素还原（0 透明 / 255 不透明）。重复取值 → 错误码 5 |
| 2/3 | `Gamma float64` | 0（关闭） | `out = round(255*(x/255)^(1/Gamma))`；开启须为正的有限值，NaN/±Inf → 错误码 6，负数 → 错误码 3 |
| 3/3 | `Dither int` | 0（关闭） | 4×4 Bayer 有序抖动：`out = clamp(round(x + (m/16-0.5)*Dither))`；确定性、无随机数；幅度取值 `[0,255]` |

逐步关闭时该步是恒等变换，因此三步全关时编码字节与引入处理链之前**逐字节相同**：
`testdata/golden/{pgm,bmp,rle}.bin` 三份锁定样本逐一 `cmp` 通过。

## 错误码

| 码 | 哨兵错误 | 字段 | 触发 |
|---:|:---:|:---:|---|
| 3 | `ErrBadOption` | `MaskValues` / `Gamma` / `Dither` | 掩码值越界、伽马为负、抖动越界 |
| 5 | `ErrMaskDuplicate` | `MaskValues` | 掩码值集合里有重复项 |
| 6 | `ErrGammaNonFinite` | `Gamma` | 伽马为 NaN 或 ±Inf |
| 7 | `ErrMaskMismatch` | `MaskValues` | 解码请求的掩码值与流中记录的不一致 |
| 8 | `ErrBadOrder` | `order` | 顺序不是三步的一个排列 |

## 顺序敏感的对照

样本 `artifacts/chain/input.pgm`（16×8、像素值覆盖 0..255），参数
`--mask 0,32,64,96,128,160,192,224 --gamma 2.2 --dither 160`。

两行对照（由 `go test ./api -run TestChainOrderSensitivity` 生成，见 `artifacts/chain/diff.txt`）：

| 行 | 顺序 | 字节数 | sha256 前 8 字节 |
|:---:|:---|---:|:---|
| 1 | `mask>gamma>dither`（记录顺序） | 172 | `46eae65cd4ae31d9` |
| 2 | `gamma>mask>dither`（调换顺序） | 172 | `0a937673c0f705e5` |

全部 5 种调换相对记录顺序的字节差分（偏移从 0 起算）：

| 调换顺序 | 首差偏移（第几个字节） | 差异字节总数 |
|:---|---:|---:|
| `mask>dither>gamma` | 14（第 15 个） | 109 |
| `gamma>mask>dither` | 157（第 158 个） | 6 |
| `gamma>dither>mask` | 156（第 157 个） | 5 |
| `dither>mask>gamma` | 14（第 15 个） | 119 |
| `dither>gamma>mask` | 14（第 15 个） | 121 |

任意一种调换都与记录顺序不同，所以每一步的先后都真的影响输出。

两次调换顺序之间的字节差分：

| | 顺序 | 字节数 | sha256 前 8 字节 |
|:---:|:---|---:|:---|
| A | `gamma>mask>dither` | 172 | `0a937673c0f705e5` |
| B | `dither>gamma>mask` | 172 | `e7fdc95aee6ada0b` |

首差字节偏移 **14**（即第 15 个字节），差异字节总数 **120**。

## 落盘存档

| 文件 | 内容 |
|---|---|
| `artifacts/chain/order_mask_gamma_dither.pgm` | 记录顺序（掩码→伽马→抖动）的完整字节流 |
| `artifacts/chain/order_gamma_mask_dither.pgm` | 调换顺序（伽马→掩码→抖动）的完整字节流 |
| `artifacts/chain/order_dither_gamma_mask.pgm` | 第二种调换顺序（抖动→伽马→掩码）的完整字节流 |
| `artifacts/chain/diff.txt` | 上面的对照两行、各调换的差分与逐字节明细 |
| `artifacts/chain/twelve-groups.md` | 十二组（3 格式 × 4 开关组合）的字节数、均值、透明像素、差分、摘要 |
| `artifacts/chain/cli/run.log` | `gtk encode` / `gtk decode` 两个子命令的实测记录 |

命令行复跑并与存档比对：

```
make build
bash artifacts/chain/run_cli.sh        # 末尾应为「结论: 全部 IDENTICAL，通过」
```

## 十二组

3 种格式 × 4 种开关组合（C0 全关 / C1 掩码 / C2 掩码+伽马 / C3 掩码+伽马+抖动），
每组给出字节数、解码均值、透明像素数、相对全关的首差偏移与差异字节数、摘要。
完整表见 `artifacts/chain/twelve-groups.md`，由 `go test ./api -run TestTwelveGroups -v` 生成。

## 解码侧说明

解码只能逆掩码与伽马：掩码透明位图从尾段还原到 `Alpha`，伽马按 `1/Gamma` 逆变换。
抖动是有损的显示变换（把量化台阶打散），没有原始值可逆，因此 `Dither>0` 时解码结果
保留抖动后的像素，往返误差在十二组表的「解码均值」一列可见。
