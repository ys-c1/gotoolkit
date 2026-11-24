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

## 校验和（可选）

文件损坏过去只能整份重解才发现；现在可以在文件头写入多项式校验（CRC），
解码时先验证，改动任意一字节都会被检出并给出精确到字节的偏移。

- 可配项都在公开结构 `api.Options`：`Checksum` 开关、`Polynomial` 多项式名字
  （`crc32-ieee` 32 位档，默认；`crc16-ibm` 16 位档）。CLI 对应
  `--checksum` 与 `--polynomial`。非法多项式名字会被拒绝，错误中给出该名字。
- 校验按文件体计算（覆盖除校验字段本身外的全部字节：头+体），写进文件头：
  pgm 写头部第二行定宽注释 `#GTKC32 xxxxxxxx`，bmp 写头内 6..9 保留字节，
  rle 魔数升为 `GTR2` 并把校验写在其后。
- 多项式选取依据：每个位宽档都选该档最常用、参数有公开标准来源的多项式——
  `crc32-ieee` 即 CRC-32/ISO-HDLC，多项式 0x04C11DB7（反射 0xEDB88320），
  来源 ITU-T V.42 / RFC 1952（zlib、PNG、Ethernet 同款）；
  `crc16-ibm` 即 CRC-16/IBM（ARC），多项式 0x8005（反射 0xA001），
  来源 IBM（Modbus、USB 的 16 位校验同族）。
- 定位原理：CRC 对等长消息是线性的，失配差分只取决于差分字节与其位置，
  从流尾向前 O(n) 枚举即可找到被改字节（`internal/checksum`）。
- 检出率（逐档）：
  - 32 位档（crc32-ieee）：任意单字节改动 100% 检出且定位唯一（突发长 8 ≤ 32 必检；
    候选数 ≈ n·255/2³²，16 MiB 以内实际唯一）；任意多字节损坏检出率 ≥ 1−2⁻³² ≈ 99.99999998%。
  - 16 位档（crc16-ibm）：任意单字节改动 100% 检出（8 ≤ 16 必检）；
    多字节损坏检出率 ≥ 1−2⁻¹⁶ ≈ 99.9985%；症候空间只有 2¹⁶，
    文件较大时定位可能给出多个候选偏移（生日界 n·255 ≳ 2¹⁶），全部如实列出。
  - 实测（`TestChecksumDetectionRateSingleByte`，三种格式各约 3k–9k 次单字节翻转）：两档均 100% 检出。
- 耗时（逐档，1 MiB 文件体，i7-13620H，`go test -bench Sum`）：
  - crc32-ieee：约 137 µs/MiB（≈7.6 GB/s）
  - crc16-ibm：约 137 µs/MiB（≈7.7 GB/s）
  两档都是逐字节查表，耗时基本相等，差异可忽略。
- 校验开关关闭时，三种格式输出与 `testdata/golden/` 基准逐字节相同
  （`TestGoldenBytes`、`TestChecksumOffByteIdenticalToGolden` 守护）。

