# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准
- `internal/guard/` 基准漂移守卫，`cmd/guard/` 是它的命令行入口

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## 漂移守卫

```
make guard          # 或 go run ./cmd/guard
```

守卫把基准的"真值"两边对齐，任一对不上都以非零退出：

- **基准对照**：在临时目录里跑 `ref/` 重新生成三份基准，与 `testdata/golden` 逐字节比对；
- **实现对照**：用 `api` 对同一样本编码，与 `testdata/golden` 逐字节比对。

比的都是字节，不一致就报首个差异偏移；对不上的格式再把两侧解码出的图并排
（左期望 / 右实际），逐像素指出差在哪些坐标、哪些灰度替换。字节结论与像素
结论写在同一份输出里，并排图落在 `artifacts/drift-guard/`（`--out` 可改）。

`testdata/golden/` 与 `ref/` 只读：守卫从不写它们，也不会"修正"基准；
对不上只能改实现。返回码只有 0（全一致）与 1（漂移或守卫自身无法完成）。

