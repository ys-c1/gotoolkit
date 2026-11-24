# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 格式实现（pgm/bmp/rle/idx），外部不可引用
- `internal/imgfx/` 缩略图链路的像素处理（面积平均缩略、调色板索引/抖动）
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## 缩略图链路

三件事，固定顺序：**面积平均缩略 → 调色板索引（索引不去重）**，抖动在索引阶段。

- `--thumb N`：按 N×N 分块做面积平均（块内求和除以块内像素数，四舍五入），
  缩略后再索引；`0` 不缩略，负数报错（`api.ErrBadThumbDiv`）。
- `--palette`：量化到 `--buckets` 个等间距灰度级；桶数为零报错
  （`api.ErrZeroBuckets`）。索引每像素一项，**不去重**，数量等于像素总数。
- `--dither`：Floyd-Steinberg 误差扩散，仅在 `--palette` 打开时生效。
  缩略比例与抖动强度关系：**强度 = 1/thumb**（未缩略时为 1），除数越大抖动越弱。
- `--format idx`：输出 `GTID` 索引容器（头 + 调色板 + 每像素索引），必须配合 `--palette`。

CLI 会按缩略前/后两列打印索引数量，并给出可复算的差值（后 − 前）。
所有新开关默认关闭；全关时三种常规格式的产物与旧版逐字节一致。

## 验证

```
make test       # go vet + 全部单测（含十二组矩阵与 golden 字节校验）
make check      # 构建后跑 scripts/ 下的十二组端到端检查
```

十二组 = 缩略状态（不缩略 / 除数 2 / 除数 3）× 调色板开/关 × 抖动开/关，缺一组不算完成。
