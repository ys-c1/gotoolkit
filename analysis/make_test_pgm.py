#!/usr/bin/env python3
# 生成抖动对比用的确定性测试图（P5 PGM）：
# 上半为均匀灰场 128（暴露规则纹路），下半为水平渐变 0..255（暴露梯度带纹）。
# 运行: python3 -I analysis/make_test_pgm.py <out.pgm> [size]
import sys


def main():
    out = sys.argv[1]
    size = int(sys.argv[2]) if len(sys.argv) > 2 else 256
    pix = bytearray(size * size)
    half = size // 2
    for y in range(size):
        for x in range(size):
            if y < half:
                v = 128
            else:
                v = x * 255 // (size - 1)
            pix[y * size + x] = v
    header = f"P5\n{size} {size}\n255\n".encode()
    with open(out, "wb") as f:
        f.write(header + bytes(pix))
    print(f"wrote {out} ({size}x{size})")


if __name__ == "__main__":
    main()
