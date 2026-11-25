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

## 区间映射的平台与可逆性

8/10/12 位深（\(N=2^b\) 级）灰度量化到 \(M=256\) 个等宽区间
\(T(x)=\lfloor Mx/N\rfloor\) 后反解：取整在 \(M<N\) 时必然压出平台。
平台段用**相邻映射值之差为零**判定；可逆当且仅当无零差边（最长平台为 1），
并证明 \(M\ge N\) 时可逆、最小区间数 \(M_{\min}=N\)（256/1024/4096）。
证明、充要条件与三档表见
[`docs/plateau-reversibility.md`](docs/plateau-reversibility.md)。

```
make roundtrip   # 生成 CSV/JSON 与 artifacts/roundtrip_<bits>bit.png 三张往返图
make test        # internal/grayquant 锁定平台长度与往返像素差异 0/3072/3840
```

- `internal/grayquant/` 模型、相邻差判据、充要条件与全部数字的单元测试
- `artifacts/roundtrip_{8,10,12}bit.png` 每档一份往返对照图；可逆的 8bit 档差异为 **0 像素**

