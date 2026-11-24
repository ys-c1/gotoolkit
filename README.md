# gotoolkit

分层的灰度图编解码工具，用于出题基线。

- `api/` 唯一对外入口，`Options` 是可配参数的公共声明处
- `internal/codec/` 三个格式实现，外部不可引用
- `cmd/gtk/` 命令行，`encode` 与 `decode` 两条子命令
- `testdata/golden/` 三种格式的基准字节，由 `ref/` 生成，**不许改**
- `ref/` 独立参照，只用来生成基准

```bash
make test
make build
./build/gtk encode --format bmp --in a.pgm --out a.bmp
./build/gtk decode --format bmp --in a.bmp --out b.pgm
```

## 坏段检测与可视化恢复

PGM P5/maxval=255 中任意字节都可能是合法灰度，所以检测必须依赖可信的旁路
SHA-256 分块清单（或实验时持有的原图）。默认编解码输出和 `testdata/golden/`
保持不变；清单是可选能力。

```bash
# 生成三档坏段实验、比例表和品红标记图
make lab

# 或手动：先生成清单，再检测并恢复
./build/gtk manifest --in artifacts/corruption-lab/source.pgm --out /tmp/m.json
./build/gtk recover \
  --in artifacts/corruption-lab/tier2-256B/corrupted.pgm \
  --manifest /tmp/m.json \
  --original artifacts/corruption-lab/source.pgm \
  --out /tmp/recovered.png --report /tmp/recover.json
```

恢复数组使用 `api.RecoveryMarker = 256` 标记坏像素；256 不在灰度域 `0..255`。
PNG/PPM 可视化把它渲染成品红 `RGB(255,0,255)`，三通道不等，因此也不是灰度。
只能输出 8-bit PGM 时，兜底标记为 `255`；若原图本身可能含 255，必须同时使用
`bad-mask.pgm` 区分。完整三档坏段位置、长度、理论/实测字节恢复比例见
[`artifacts/corruption-lab/README.md`](artifacts/corruption-lab/README.md)。
