"""分析：函数分解、单调性符号检验、Pareto 前沿与支配关系验证。

输出：
  artifacts/summary_table.csv  四档各占一行的宽表
  artifacts/analysis_summary.json  机器可读结论
并在标准输出打印 Markdown 表格与检验结论。
"""
import csv
import json
import math
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import numpy as np

from common import read_pgm

ROOT = os.path.join(os.path.dirname(__file__), "..")
RESULTS = os.path.join(ROOT, "artifacts", "results.csv")
IMG_DIR = os.path.join(ROOT, "artifacts", "images")
BLOCKS = [2, 4, 8, 16]
GTBK_HEADER = 9
BLOCK_HEADER = 4


def load_results():
    with open(RESULTS, encoding="utf-8") as f:
        rows = list(csv.DictReader(f))
    for r in rows:
        r["block"] = int(r["block"])
        r["volume_bytes"] = int(r["volume_bytes"])
        r["blockiness_gray"] = float(r["blockiness_gray"])
        r["boundary_jump_decoded_gray"] = float(
            r["boundary_jump_decoded_gray"])
    return rows


def volume_decomposition(image_path: str, b: int) -> tuple[int, int, int]:
    """按 GTBK 编码规则、用与 Go 完全一致的整数运算独立复算体积。

    均值 = (块内整数和 + N/2) // N（加半舍入），残差裁剪到 [-128,127]，
    位深 k 为可容纳残差绝对值的最小位数（1..8）。拆成块头项与残差载荷项。
    """
    img = read_pgm(image_path)  # uint8，整数像素
    h, w = img.shape
    nbh, nbw = h // b, w // b
    n = b * b
    header_bytes = BLOCK_HEADER * nbh * nbw
    payload_bytes = 0
    for by in range(nbh):
        for bx in range(nbw):
            blk = img[by * b:(by + 1) * b, bx * b:(bx + 1) * b].astype(np.int64)
            mean = min(255, (int(blk.sum()) + n // 2) // n)
            amax = 0
            for v in blk.reshape(-1):
                r = int(v) - mean
                r = max(-128, min(127, r))
                amax = max(amax, abs(r))
            if amax == 0:
                k = 1
            else:
                k = min(8, math.ceil(math.log2(2 * amax + 1)))
            payload_bytes += math.ceil(n * k / 8)
    return header_bytes, payload_bytes, GTBK_HEADER + header_bytes + payload_bytes


def main() -> None:
    rows = load_results()
    images = sorted({r["image"] for r in rows})
    index = {(r["image"], r["block"]): r for r in rows}

    # ---- 1. 体积函数分解，并与真实文件大小对账 ----
    decomp = {}
    for img in images:
        for b in BLOCKS:
            hdr, pay, total = volume_decomposition(os.path.join(IMG_DIR, img), b)
            actual = index[(img, b)]["volume_bytes"]
            assert total == actual, (
                f"{img} B={b} 复算体积 {total} != 实际文件 {actual}")
            decomp[(img, b)] = {"header": hdr, "payload": pay, "total": total}

    # ---- 2. 单调性符号检验 ----
    signs = {}
    for img in images:
        for b0, b1 in zip(BLOCKS[:-1], BLOCKS[1:]):
            d_v = index[(img, b1)]["volume_bytes"] - index[(img, b0)]["volume_bytes"]
            d_q = index[(img, b1)]["blockiness_gray"] - index[(img, b0)]["blockiness_gray"]
            signs[(img, b0, b1)] = {
                "delta_volume": d_v,
                "delta_blockiness": round(d_q, 4),
                "opposite_direction": (d_v < 0 < d_q) or (d_q < 0 < d_v),
            }
    q_monotone = all(s["delta_blockiness"] > 0 for s in signs.values())
    front_opposite = all(
        signs[(img, b0, b1)]["opposite_direction"]
        for img in images for b0, b1 in [(2, 4), (4, 8)])

    # ---- 3. Pareto 前沿（双目标都求最小）----
    fronts = {}
    for img in images:
        pts = [(b, index[(img, b)]["volume_bytes"],
                index[(img, b)]["blockiness_gray"]) for b in BLOCKS]
        front = []
        for p in pts:
            dominated = any(
                q is not p
                and q[1] <= p[1] and q[2] <= p[2]
                and (q[1] < p[1] or q[2] < p[2])
                for q in pts)
            if not dominated:
                front.append(p[0])
        fronts[img] = sorted(front)

    # ---- 4. 选中档 B=8 对相邻档 B=16 的双指标支配，三组数据逐一验证 ----
    dominance = {}
    for img in images:
        p8, p16 = index[(img, 8)], index[(img, 16)]
        dominance[img] = {
            "volume_b8_better": p8["volume_bytes"] < p16["volume_bytes"],
            "blockiness_b8_better": p8["blockiness_gray"] < p16["blockiness_gray"],
            "volume_saved_bytes": p16["volume_bytes"] - p8["volume_bytes"],
            "blockiness_reduced_gray":
                round(p16["blockiness_gray"] - p8["blockiness_gray"], 3),
        }
    knee_dominates = all(
        d["volume_b8_better"] and d["blockiness_b8_better"]
        for d in dominance.values())

    # ---- 5. 宽表：四档各占一行 ----
    wide_path = os.path.join(ROOT, "artifacts", "summary_table.csv")
    labels = {r["image"]: r["label"] for r in rows}
    with open(wide_path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        header = ["块长B"]
        for img in images:
            header += [f"{labels[img]} 体积/字节", f"{labels[img]} 方块感/灰度级"]
        w.writerow(header)
        for b in BLOCKS:
            line = [b]
            for img in images:
                r = index[(img, b)]
                line += [r["volume_bytes"], f"{r['blockiness_gray']:.3f}"]
            w.writerow(line)

    summary = {
        "blocks": BLOCKS,
        "blockiness_monotone_increasing": q_monotone,
        "opposite_monotonic_direction_on_front_B2_to_B8": front_opposite,
        "pareto_front_by_image": fronts,
        "selected_knee_block": 8,
        "knee_B8_dominates_B16_on_all_three_images": knee_dominates,
        "dominance_detail": dominance,
        "sign_table": {f"{k[0]}|{k[1]}->{k[2]}": v for k, v in signs.items()},
        "volume_decomposition": {
            f"{k[0]}|B={k[1]}": v for k, v in decomp.items()},
    }
    with open(os.path.join(ROOT, "artifacts", "analysis_summary.json"),
              "w", encoding="utf-8") as f:
        json.dump(summary, f, ensure_ascii=False, indent=2)

    # ---- 控制台报告 ----
    print("## 四档体积与方块感（各占一行）\n")
    print("| 块长 B |", " | ".join(
        f"{labels[i]} V/字节 | {labels[i]} Q/灰度级" for i in images), "|")
    print("|" + "---|" * (1 + 2 * len(images)))
    for b in BLOCKS:
        cells = [str(b)]
        for img in images:
            r = index[(img, b)]
            cells += [str(r["volume_bytes"]), f"{r['blockiness_gray']:.3f}"]
        print("| " + " | ".join(cells) + " |")

    print("\n## 相邻档差分符号（ΔV 字节 / ΔQ 灰度级）\n")
    print("| 图像 | 区间 | ΔV | ΔQ | 方向相反 |")
    print("|---|---|---|---|---|")
    for img in images:
        for b0, b1 in zip(BLOCKS[:-1], BLOCKS[1:]):
            s = signs[(img, b0, b1)]
            print(f"| {labels[img]} | {b0}->{b1} | {s['delta_volume']:+d} | "
                  f"{s['delta_blockiness']:+.3f} | {s['opposite_direction']} |")

    print("\n## 体积函数分解（块头项 ∝ 1/B²，残差项随 B 增大）\n")
    print("| 图像 | B | 块头项/字节 | 残差项/字节 | 合计 |")
    print("|---|---|---|---|---|")
    for img in images:
        for b in BLOCKS:
            d = decomp[(img, b)]
            print(f"| {labels[img]} | {b} | {d['header']} | "
                  f"{d['payload']} | {d['total']} |")

    print("\n## Pareto 前沿与支配验证\n")
    for img in images:
        print(f"- {labels[img]} 前沿块长: {fronts[img]}")
    print(f"- Q 随 B 严格单调上升: {q_monotone}")
    print(f"- 前沿区间 B=2→8 内 V 下降、Q 上升，方向相反: {front_opposite}")
    print(f"- 选中档 B=8 在三组数据上均双指标支配 B=16: {knee_dominates}")
    for img in images:
        d = dominance[img]
        print(f"  - {labels[img]}: 省 {d['volume_saved_bytes']} 字节, "
              f"方块感降 {d['blockiness_reduced_gray']:.3f} 灰度级")


if __name__ == "__main__":
    main()
