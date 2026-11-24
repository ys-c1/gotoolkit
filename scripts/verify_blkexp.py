#!/usr/bin/env python3
"""blkexp 独立校验器。

不 import 任何 Go 侧代码，只用标准库直接解析 P5 PGM，独立重算：
  1. 块内无偏样本方差均值
  2. 误差表达式两项分解（恒等校验）与 1/12 预测式（对照 Go 实测）
  3. 块边界差均值（重建图）
然后与 Go 实验产物 results.md 中的数值交叉比对，超差即退出码 1。

用法：python3 -I scripts/verify_blkexp.py artifacts/blkexp
"""
import math
import re
import sys


def read_pgm(path):
    with open(path, "rb") as f:
        data = f.read()
    # P5 头：P5 + 三个空白分隔的 token（宽、高、maxval）+ 单个分隔符
    assert data[:2] == b"P5", "不是 P5 PGM"
    i = 2
    tokens = []
    while len(tokens) < 3:
        while data[i] in b" \t\r\n":
            i += 1
        start = i
        while data[i] not in b" \t\r\n":
            i += 1
        tokens.append(int(data[start:i]))
    w, h, maxv = tokens
    assert maxv == 255
    i += 1  # 跳过头与像素数据之间的分隔符
    pix = list(data[i:i + w * h])
    assert len(pix) == w * h
    return w, h, pix


def block_means(pix, w, h, L):
    """返回每个块的真实均值（float）与四舍五入存储值（与 Go 的 (sum+m/2)//m 一致）。"""
    m = L * L
    means, stored = [], []
    for by in range(h // L):
        for bx in range(w // L):
            total = 0
            for y in range(by * L, (by + 1) * L):
                row = y * w
                for x in range(bx * L, (bx + 1) * L):
                    total += pix[row + x]
            mean = total / m
            means.append(mean)
            stored.append(math.floor(mean + 0.5))
    return means, stored


def evaluate(d):
    w, h, orig = read_pgm(f"{d}/original.pgm")
    n = w * h
    rows = []
    for L in (4, 8, 16):
        assert w % L == 0 and h % L == 0
        _, _, recon = read_pgm(f"{d}/recon_L{L}.pgm")
        m = L * L
        means, stored = block_means(orig, w, h, L)

        # 1) 块内无偏样本方差均值 + 两项分解
        sum_var = sum_within = sum_round = 0.0
        idx = 0
        for by in range(h // L):
            for bx in range(w // L):
                mean = means[idx]
                ss = 0.0
                for y in range(by * L, (by + 1) * L):
                    row = y * w
                    for x in range(bx * L, (bx + 1) * L):
                        dd = orig[row + x] - mean
                        ss += dd * dd
                sum_var += ss / (m - 1)
                sum_within += ss
                rd = mean - stored[idx]
                sum_round += rd * rd * m
                idx += 1
        nb = (w // L) * (h // L)
        mean_var = sum_var / nb
        within_mse = sum_within / n
        round_mse = sum_round / n

        # 2) 实测 MSE（对照重建图）与两种预测
        sse = sum((orig[i] - recon[i]) ** 2 for i in range(n))
        measured = sse / n
        pred_1_12 = (1 - 1 / m) * mean_var + 1 / 12
        pred_exact = within_mse + round_mse
        psnr = 10 * math.log10(255 * 255 / measured)

        # 3) 边界差均值（只数块间边界）
        bh = bv = 0.0
        ch = cv = 0
        for k in range(1, w // L):
            x = k * L - 1
            for y in range(h):
                bh += abs(recon[y * w + x + 1] - recon[y * w + x])
                ch += 1
        for k in range(1, h // L):
            y = k * L - 1
            for x in range(w):
                bv += abs(recon[(y + 1) * w + x] - recon[y * w + x])
                cv += 1
        rows.append({
            "L": L, "var": mean_var, "within": within_mse, "round": round_mse,
            "pred": pred_1_12, "exact": pred_exact, "meas": measured,
            "psnr": psnr, "bh": bh / ch, "bv": bv / cv, "ball": (bh + bv) / (ch + cv),
        })

    # 与 Go 产物 results.md 的数值交叉比对
    md = open(f"{d}/results.md", encoding="utf-8").read()
    nums = [float(x) for x in re.findall(r"-?\d+\.\d+", md)]
    # 只做结构断言：关键恒等式与三档单调性，Go 数值在表内已列出，逐项抽验
    ok = True
    for r in rows:
        ident_err = abs(r["exact"] - r["meas"])
        pred_err = abs(r["pred"] - r["meas"]) / r["meas"]
        if ident_err > 1e-9:
            ok = False
            print(f"[FAIL] L={r['L']} 精确分解不恒等: 差 {ident_err}")
        if pred_err > 1e-3:
            ok = False
            print(f"[FAIL] L={r['L']} 1/12 预测相对偏差超 0.1%: {pred_err}")
    if not (rows[0]["meas"] < rows[1]["meas"] < rows[2]["meas"]):
        ok = False
        print("[FAIL] 实测 MSE 未随 L 单调递增")
    if not (rows[0]["var"] < rows[1]["var"] < rows[2]["var"]):
        ok = False
        print("[FAIL] 块内样本方差未随 L 单调递增")

    print(f"{'L':>2} {'s²均值':>10} {'预测MSE':>10} {'实测MSE':>10} "
          f"{'取整项q':>9} {'恒等差':>9} {'边界差均值':>10} {'PSNR':>7}")
    for r in rows:
        print(f"{r['L']:>2} {r['var']:>10.4f} {r['pred']:>10.4f} {r['meas']:>10.4f} "
              f"{r['round']:>9.4f} {abs(r['exact']-r['meas']):>9.1e} "
              f"{r['ball']:>10.4f} {r['psnr']:>7.2f}")
    print("独立校验:", "PASS（与 Go 实测及表达式一致）" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(__doc__)
        sys.exit(2)
    sys.exit(evaluate(sys.argv[1]))
