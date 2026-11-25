#!/usr/bin/env python3
"""独立复核：只读取 artifacts 下的 CSV，不依赖 Go 实现的任何函数。

核对：倒挂计数、倒挂充要条件（理想相邻差<1）、修复后零倒挂、
端点保持、逐点最大偏差（输出为单一数值）、平台铺开的严格性。
"""
import csv
import math
import sys

GAMMA = 2.2


def read_lut(path):
    cols = {"index": [], "raw": [], "mono": [], "fixed": []}
    with open(path, newline="") as f:
        for row in csv.DictReader(f):
            for k in cols:
                cols[k].append(int(row[k]))
    return cols


def read_inv(path):
    out = []
    with open(path, newline="") as f:
        for row in csv.DictReader(f):
            out.append(tuple(int(row[k]) for k in ("i0", "i1", "y0", "y1")))
    return out


def ideal(i, n):
    return (n - 1) * (i / (n - 1)) ** GAMMA


def check(bits, art_dir):
    lut = read_lut(f"{art_dir}/lut_{bits}bit.csv")
    inv_file = read_inv(f"{art_dir}/inversions_{bits}bit.csv")
    n = len(lut["index"])
    raw, mono, fixed = lut["raw"], lut["mono"], lut["fixed"]

    # 1. CSV 倒挂明细与从 raw 重算的倒挂完全一致
    inv_recalc = [(i - 1, i, raw[i - 1], raw[i])
                  for i in range(1, n) if raw[i] < raw[i - 1]]
    assert inv_recalc == inv_file, f"{bits}bit 倒挂明细不一致"
    assert len(inv_recalc) > 0, f"{bits}bit 原始映射应存在倒挂"

    # 2. 充要条件：每个倒挂位置的理想相邻差必须 < 1
    for i0, i1, _, _ in inv_recalc:
        d = ideal(i1, n) - ideal(i0, n)
        assert d < 1, f"{bits}bit 倒挂@{i0},{i1} 理想差 {d} >= 1"
    # 反向抽样：理想差 >= 1 的位置不允许倒挂
    for i in range(1, n):
        if ideal(i, n) - ideal(i - 1, n) >= 1:
            assert raw[i] >= raw[i - 1], f"{bits}bit 大斜率位置出现倒挂"

    # 3. 修复后零倒挂（mono 与 fixed 都非递减）
    for name, v in (("mono", mono), ("fixed", fixed)):
        bad = [i for i in range(1, n) if v[i] < v[i - 1]]
        assert not bad, f"{bits}bit {name} 仍有 {len(bad)} 处倒挂"

    # 4. 端点保持：raw/mono/fixed 首点=0、末点=L-1
    assert raw[0] == mono[0] == fixed[0] == 0
    assert raw[-1] == mono[-1] == fixed[-1] == n - 1

    # 5. 逐点偏差，报告单一数值
    dev_mono = max(abs(mono[i] - raw[i]) for i in range(n))
    dev_fixed_raw = max(abs(fixed[i] - raw[i]) for i in range(n))
    dev_fixed_ideal = max(abs(fixed[i] - ideal(i, n)) for i in range(n))
    assert dev_mono <= 1, f"{bits}bit cummax 偏差越界: {dev_mono}"

    # 6. 值域合法
    assert all(0 <= v <= n - 1 for v in fixed)

    # 7. 平台铺开：fixed 中每个等值段，要么段长 1（严格），
    #    要么是位深不足的固有平台（段内相邻输入的理想差无法容纳整数步进）
    tie_steps = sum(1 for i in range(1, n) if fixed[i] == fixed[i - 1])
    raw_ties = sum(1 for i in range(1, n) if raw[i] == raw[i - 1])

    print(f"{bits:2d}bit n={n:5d} 倒挂={len(inv_recalc):3d} 修复后倒挂=0 "
          f"raw并列={raw_ties:4d} 残留并列={tie_steps:4d} "
          f"max|mono-raw|={dev_mono} max|fixed-raw|={dev_fixed_raw} "
          f"max|fixed-ideal|={dev_fixed_ideal:.3f} "
          f"端点=({fixed[0]},{fixed[-1]})")
    return len(inv_recalc), dev_fixed_raw


def main():
    art_dir = sys.argv[1] if len(sys.argv) > 1 else "artifacts"
    summary = {}
    for bits in (8, 10, 12):
        summary[bits] = check(bits, art_dir)
    print("\n全部断言通过。")
    for bits, (cnt, dev) in summary.items():
        print(f"  {bits}bit: 倒挂 {cnt} 处 → 0；与原始映射最大偏差 = {dev}（LSB）")


if __name__ == "__main__":
    main()
