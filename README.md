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

## 处理链（顺序固定）

三段处理合成一条链，顺序是**固定语义**，写进 `api.Options` 字段注释、`gtk encode|decode --help` 与文档：

```
输入 → [1] 透明掩码 --mask → [2] 伽马 --gamma → [3] 抖动 --dither → 输出
```

| 步 | 参数 | 默认 | 说明 |
|:---:|:---:|---:|---|
| 1/3 | `--mask V[,V...]` | 关闭 | 掩码灰度值集合；解码后还原为 `Image.Alpha`；重复取值报错误码 5 |
| 2/3 | `--gamma V` | 0（关闭） | `out = round(255*(x/255)^(1/V))`；非有限值报错误码 6 |
| 3/3 | `--dither N` | 0（关闭） | 4×4 有序抖动，幅度 `[0,255]`；确定性无随机数 |

全部关闭时编码字节与引入处理链之前**逐字节相同**，`testdata/golden` 三份基准逐一 cmp 通过。
`--order A>B>C` 只用于复现顺序差异，默认即记录顺序 `mask>gamma>dither`；非法顺序报错误码 8。

顺序对照两行、各调换的字节差分（精确到第几个字节）、十二组指标与命令行实录见
`docs/chain.md` 与 `artifacts/chain/`。

