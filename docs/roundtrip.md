# 三格式往返校验

三种格式过去各自写头：PGM 的 ASCII 头写宽高与最大值，BMP 的 54 字节头写宽高、
通道数、行补齐，RLE 的 `GTR1` 头用两字节小端写宽高。头字段没有统一口径，
一张图转来转去，像素变了也未必有人发现。

本校验把「一圈往返」拆成四项可数的断言，每种格式各出一份报告，三份合并成
一次总判定。任一份不一致，总判定即为失败。

## 一圈的定义

```
源图 --Encode--> 码流 --ParseHeader--> 头字段
                   |
                   +--Decode--> 解码图 --逐像素--> 与源图比
                                  |
                                  +--Encode--> 重编码码流 --逐字节--> 与码流比
```

四项断言：

| 项 | 口径 | 通过条件 |
|---|---|---|
| 像素 | 解码图与源图逐像素 | 差异 0 个，最大差 0 |
| 头字段 | 码流自身的宽、高、位深 | 与源图相等 |
| 三头互比 | 三份头横向 | 宽、高、位深两两相同 |
| 重编码字节 | 解码结果再编码 | 与进入解码器的码流逐字节相同 |

「不做改动时 encode 的字节保持原样」由 `TestGoldenBaselineUntouched` 锁定：
三格式对 `testdata/golden` 的同一张 4×3 样本输出与基准逐字节一致。

头字段全部从**码流本身**解析（`ParseHeader`），不复用 `internal/codec` 的解码
路径，避免解码器的理解偏差掩盖头的不一致。

位深口径统一为「每个灰度样本的有效位数」，三种格式都是 8。BMP 容器每像素
实际占 24 位（R/G/B 三通道同值），报告里另列，不参与一致性判定。

## 报告

```
go run ./cmd/roundtrip -out artifacts/roundtrip
make roundtrip
```

`make test` 会顺带重生成 `artifacts/roundtrip/`，缺失即测试失败。

| 文件 | 内容 |
|---|---|
| `pgm.txt` / `bmp.txt` / `rle.txt` | 各格式一份，字节数与差异数并列 |
| `report.txt` | 三份合并 + 总判定 |
| `source.pgm` | 往返前的源图（PGM 字节流） |
| `roundtrip.pgm` | 往返后的图（三格式解码结果，通过时两两相同） |

两张结果就是 `source.pgm` 与 `roundtrip.pgm`。差异用数字说：报告里每个格式
一节，给出「像素差异 / 字节差异 / 最大差 / 首差偏移」；总判定给出三份之和与
跨格式像素差异。校验通过时这些数字全为 0，`cmp` 两张图为 IDENTICAL。

样本 `DefaultSample()` 是 37×23（851 像素）：宽度 37 让 BMP 每行 111 字节、
补齐到 112，覆盖行补齐；前 300 像素同值，使 RLE 触到单条游程 255 的上限。

## 故障注入

测试里用改坏的码流验证数字是真实的，不是恒零：

| 测试 | 改动 | 期望数字 |
|---|---|---|
| `TestPixelDiffIsCounted` | PGM 第 6 个像素改一位 | 像素差异 1，最大差 64，首差偏移 5 |
| `TestByteDiffIsCounted` | BMP 第一行补齐字节改为 0x7f | 像素差异 0，字节差异 1，首差偏移 165 |
| `TestHeaderMismatchDetected` | BMP 头高 +1 | 头高不一致，判定失败 |
| `TestCombineFailsOnMissingReport` | 缺 rle 报告 | 总判定失败，问题点名缺 rle |
| `TestCombineFailsOnAnyIssue` | 一份报告像素差异 3 | 总判定失败，汇总差异 3 |
| `TestCombineFailsOnCrossFormatPixels` | bmp 解码结果改一像素 | 跨格式差异 1，判定失败 |

## 拒绝口径

- 三份报告缺一份、重复一份：总判定失败。
- 任一份的像素 / 头字段 / 重编码字节任一不一致：总判定失败。
- 三份解码结果互相不一致：总判定失败（覆盖「各转各的、像素悄悄变了」）。
- 命令行 `cmd/roundtrip` 在总判定失败时以非零码退出。
