#!/usr/bin/env python3
"""独立交叉验证：直接解析 artifacts 下的 PGM，重算三档位深量化误差熵。

与 Go 实现不共享任何代码。用法：
    python3 -I /path/to/verify_entropy.py /path/to/artifacts
"""
import math
import os
import sys


def read_pgm(path):
    with open(path, "rb") as f:
        data = f.read()
    assert data[:2] == b"P5", "不是 P5"
    # 解析三个头字段：宽 高 maxval
    idx, vals = 2, []
    while len(vals) < 3:
        while data[idx] in b" \r\n\t":
            idx += 1
        start = idx
        while data[idx] in b"0123456789":
            idx += 1
        vals.append(int(data[start:idx]))
    idx += 1  # 单字节分隔符
    w, h, _maxv = vals
    return w, h, list(data[idx:idx + w * h])


def entropy_of_error(pix, b):
    d = b - 1
    delta = 1 << d
    counts = [0] * delta
    for x in pix:
        counts[x % delta] += 1
    n = len(pix)
    h = 0.0
    for c in counts:
        if c:
            p = c / n
            h -= p * math.log2(p)
    return h, counts


def main():
    art = sys.argv[1]
    for name in ("uniform.pgm", "uneven.pgm"):
        w, h, pix = read_pgm(os.path.join(art, name))
        assert len(pix) == w * h == 65536
        print(f"== {name} ==")
        prev = None
        for b in (2, 3, 4):
            ent, counts = entropy_of_error(pix, b)
            theory = b - 1
            diff = "" if prev is None else f"  熵增={ent - prev:.6f}"
            print(f"b={b} Δ={1 << (b - 1):3d}  H={ent:.6f} bpp  "
                  f"理论={theory}  偏差={ent - theory:+.6f}{diff}")
            prev = ent
        print()


if __name__ == "__main__":
    main()
