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

## 透明掩码

把指定灰度值当作透明掩码：编码时所有等于该灰度的像素被标为透明，解码后原始灰度零误差还原，透明标记通过 `Image.Alpha`（0 透明 / 255 不透明）返回。

- 配置入口在 `api.Options.Mask`：`-1`（缺省）关闭，`0..255` 启用并指定掩码灰度；其他值被 `ErrBadOption` 拦下。
- 掩码流在各格式原始载荷之后追加统一尾段 `GTMK`：`GTMK(4) + 掩码值(1) + 位图字节数(2,LE) + 打包位图(每像素 1 位，1=透明)`。原始像素字节不做任何替换，因此可零误差还原。
- 掩码关闭（`Mask=-1`）时走与旧版完全相同的编码路径，输出与无掩码版本逐字节一致；关闭状态也能解码带尾段的流（只取像素）。
- 掩码与增益同时开启时**以掩码为准，先掩码后增益**：先用原始像素确定透明位图，再施加亮度增益，透明判定不受增益影响。

```
./build/gtk encode --format bmp --mask 0   --in a.pgm --out a.bmp
./build/gtk decode --format bmp --mask 0   --in a.bmp --out b.pgm
```
