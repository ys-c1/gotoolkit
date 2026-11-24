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

## 缩略图（面积平均降采样）

`Options.Divisor` 是唯一的降采样开关（默认 `1`，必须 `>= 1`，`0`/负值返回
`ErrBadOption`）。命令行两个动词都支持 `--divisor D`：

```
./build/gtk encode --format bmp --in a.pgm --out thumb.bmp --divisor 2
./build/gtk decode --format bmp --in thumb.bmp --out thumb.pgm --divisor 2
```

输出画幅为 `ceil(W/D) × ceil(H/D)`，每个输出像素取对应 `D×D` 源块的**面积平均**
（块内等权求和后除以实际像素数，四舍五入），不是点采样。边界语义：

- **边缘不足整块的像素**：右/下边缘的残缺块照常产出一个像素，除数取块内实际像素数，
  不补零、不复制边缘、不丢弃。
- **边缘块权重**：块内每个实际像素等权 `1/n`；残缺块不引入虚拟填充格。
- **除数不整除的余下像素**：余下的行/列单独组成最后一个残缺块，输出边长向上取整，
  既不并进前一块也不丢弃。
- `Divisor=1` 不做算术，输出与基准逐字节相同。
