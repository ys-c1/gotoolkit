# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `internal/roundtrip/` 三格式往返校验：像素、头字段、重编码字节与总判定
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `cmd/roundtrip/` 往返报告生成器
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
make roundtrip
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## 往返校验

三种格式各写各的头，过去没有统一的往返断言。现在每张图经三种格式各转一圈，
逐像素与源图比对，并从码流本身解析宽高位深，再把解码结果重编码与原始码流
逐字节比对。三格式各出一份报告，合并成一次总判定，任一份不一致即失败。

```
make roundtrip   # 等价于 go run ./cmd/roundtrip -out artifacts/roundtrip
```

报告按格式分节、字节数与差异数并列，落在 `artifacts/roundtrip/`：
`pgm.txt`/`bmp.txt`/`rle.txt` 各一份，`report.txt` 为合并与总判定，
`source.pgm`/`roundtrip.pgm` 为往返前后的两张图（通过时 `cmp` 为 IDENTICAL）。
