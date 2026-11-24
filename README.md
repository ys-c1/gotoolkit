# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现 + 压缩链路（`pipe.go`），外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

## 压缩链路

`Options` 上有三项开关，顺序固定为 **预测编码 → 游程合并 → 体校验和**，
任一启用即输出 GTC1 容器（12 字节头 + payload）；三项全关时字节流与基准完全一致。

- 预测阶数候选集 `{1,2,3}`，`PredictOrder=0` 表示按残差熵最小自动选阶；负值或 >3 报 `ErrBadPredictOrder`
- 游程合并作用于预测后的残差流
- `Checksum="crc32"` 把体校验和（对 payload 的 CRC32）写进文件头偏移 6 处；其他值报 `ErrBadChecksum`
- 解码按 GTC1 magic 自动识别容器，篡改 payload 会报 `ErrChecksumMismatch`

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```
