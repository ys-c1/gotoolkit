#!/usr/bin/env python3
"""读取 artifacts 下的 LUT 与倒挂坐标 CSV，绘制三档位深的映射曲线图。

每档位深输出一张 PNG：主图为全码域三条曲线（原始/累积最大值/平台铺开），
内嵌暗部放大窗标注倒挂点（最多五个坐标）。配色经 dataviz 调色板校验。
"""
import csv
import os
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager

# dataviz 参考调色板（light），三槽蓝/橙/青已通过 all-pairs 校验
C_RAW = "#2a78d6"    # 原始映射（蓝）
C_MONO = "#eb6834"   # 累积最大值（橙）
C_FIX = "#1baf7a"    # 平台铺开后（青）
C_INV = "#d03b3b"    # 倒挂标记（状态红，配 X 形标记，不单靠颜色）
SURFACE = "#fcfcfb"
GRID = "#e1e0d9"
AXIS = "#c3c2b7"
INK = "#0b0b0b"
MUTED = "#52514e"

FONT_PATH = os.path.expanduser("~/.local/share/fonts/NotoSansCJKsc-Regular.ttf")
font_manager.fontManager.addfont(FONT_PATH)
FONT = font_manager.FontProperties(fname=FONT_PATH)
plt.rcParams.update({
    "font.family": FONT.get_name(),
    "axes.edgecolor": AXIS,
    "axes.labelcolor": INK,
    "text.color": INK,
    "xtick.color": MUTED,
    "ytick.color": MUTED,
    "axes.grid": True,
    "grid.color": GRID,
    "grid.linewidth": 0.8,
    "figure.facecolor": SURFACE,
    "axes.facecolor": SURFACE,
    "savefig.facecolor": SURFACE,
    "axes.unicode_minus": False,
})


def read_lut(path):
    idx, raw, mono, fixed = [], [], [], []
    with open(path, newline="") as f:
        for row in csv.DictReader(f):
            idx.append(int(row["index"]))
            raw.append(int(row["raw"]))
            mono.append(int(row["mono"]))
            fixed.append(int(row["fixed"]))
    return idx, raw, mono, fixed


def read_inversions(path):
    out = []
    with open(path, newline="") as f:
        for row in csv.DictReader(f):
            out.append((int(row["i0"]), int(row["i1"]),
                        int(row["y0"]), int(row["y1"])))
    return out


def plot_one(bits, art_dir):
    idx, raw, mono, fixed = read_lut(os.path.join(art_dir, f"lut_{bits}bit.csv"))
    invs = read_inversions(os.path.join(art_dir, f"inversions_{bits}bit.csv"))
    n = len(idx)
    lmax = n - 1

    fig, ax = plt.subplots(figsize=(11, 6.5), dpi=150)
    fig.subplots_adjust(left=0.07, right=0.78, top=0.88, bottom=0.10)

    ax.plot(idx, raw, color=C_RAW, lw=1.1, ls="-", label="原始映射（逐点取整）", zorder=2)
    ax.plot(idx, mono, color=C_MONO, lw=1.1, ls="--", label="累积最大值强制单调", zorder=3)
    ax.plot(idx, fixed, color=C_FIX, lw=1.4, ls="-", label="平台铺开后（最终映射）", zorder=4)

    # 倒挂点：红色 X 标记，落在原始曲线上（形状+颜色双重编码，不单靠颜色）
    if invs:
        ix = [p[1] for p in invs]
        iy = [raw[p[1]] for p in invs]
        ax.scatter(ix, iy, marker="X", s=46, color=C_INV,
                   edgecolor="white", linewidth=0.6, zorder=5,
                   label=f"倒挂点（共 {len(invs)} 处）")

    ax.set_xlim(0, lmax)
    ax.set_ylim(0, lmax)
    ax.set_xlabel("输入码值 i", fontproperties=FONT)
    ax.set_ylabel("输出码值", fontproperties=FONT)
    ax.set_title(
        f"{bits} 位深灰度映射（gamma 2.2，n={n}）：倒挂 {len(invs)} 处，"
        f"端点 (0,{raw[0]})→({lmax},{raw[-1]}) 保持不变",
        fontproperties=FONT, fontsize=12, pad=12,
    )
    # 图例放在图外右侧，避免与内嵌窗和曲线争夺空间
    ax.legend(prop=FONT, frameon=False, loc="upper left",
              bbox_to_anchor=(1.01, 1.0), fontsize=9)

    # 内嵌暗部放大窗（主图左上区域在该曲线下为空）
    x_hi = max(64, min(lmax // 4, max(p[1] for p in invs) * 3 + 8))
    y_hi = max(8, min(lmax // 8, max(max(p[2], p[3]) for p in invs) + 6))
    zax = ax.inset_axes([0.10, 0.52, 0.38, 0.40])
    zax.set_facecolor(SURFACE)
    m = idx.index(x_hi)
    zax.plot(idx[:m + 1], raw[:m + 1], color=C_RAW, lw=1.2, marker="o", ms=2.2)
    zax.plot(idx[:m + 1], mono[:m + 1], color=C_MONO, lw=1.2, ls="--")
    zax.plot(idx[:m + 1], fixed[:m + 1], color=C_FIX, lw=1.2)
    shown = [p for p in invs[:5] if p[1] <= x_hi]
    for (i0, i1, y0, y1) in shown:
        zax.scatter([i1], [y1], marker="X", s=55, color=C_INV,
                    edgecolor="white", linewidth=0.6, zorder=6)
    zax.set_xlim(-1, x_hi)
    zax.set_ylim(-1, y_hi)
    zax.set_title("暗部放大（倒挂点）", fontproperties=FONT, fontsize=9)
    zax.grid(True, color=GRID, linewidth=0.7)
    zax.tick_params(labelsize=7)
    # 倒挂坐标以文字块列出（最多五个），放在内嵌窗左上角空白处；
    # 三条曲线的颜色/线型身份由主图图例统一承担。
    lines = ["倒挂坐标（最多 5 个）："]
    lines += [f"({i0},{y0})→({i1},{y1})" for (i0, i1, y0, y1) in shown]
    zax.text(0.03, 0.97, "\n".join(lines), transform=zax.transAxes,
             va="top", ha="left", fontproperties=FONT, fontsize=7.5,
             color=C_INV, linespacing=1.35,
             bbox=dict(boxstyle="round,pad=0.3", fc=SURFACE,
                       ec=GRID, lw=0.7, alpha=0.92))
    ax.indicate_inset_zoom(zax, edgecolor=MUTED, linewidth=1.0)

    out = os.path.join(art_dir, f"curve_{bits}bit.png")
    fig.savefig(out)
    plt.close(fig)
    return out


def main():
    art_dir = sys.argv[1] if len(sys.argv) > 1 else "artifacts"
    for bits in (8, 10, 12):
        path = plot_one(bits, art_dir)
        print("wrote", path)


if __name__ == "__main__":
    main()
