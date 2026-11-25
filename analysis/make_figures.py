"""出图：函数曲线 + Pareto 前沿总览，以及前沿三档各自的块效应对比图。

输出到 artifacts/figures/：
  fig0_overview.png            V(B)、Q(B) 曲线 + Pareto 前沿 + 四档宽表
  fig1_compare_B2.png          块长 2 对比（原图 / 重建 / 边界跳变）
  fig2_compare_B4.png          块长 4
  fig3_compare_B8.png          块长 8
"""
import csv
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import matplotlib
matplotlib.use("Agg")
import matplotlib.font_manager as fm
import matplotlib.pyplot as plt
import numpy as np
from matplotlib.colors import LinearSegmentedColormap

from common import block_means, blockiness, read_pgm, reconstruct

ROOT = os.path.join(os.path.dirname(__file__), "..")
ART = os.path.join(ROOT, "artifacts")
FIG = os.path.join(ART, "figures")
RESULTS = os.path.join(ART, "results.csv")
BLOCKS = [2, 4, 8, 16]
FRONT = [2, 4, 8]

# dataviz 参考调色板（light 模式前三槽，已通过 validate_palette）。
BLUE, ORANGE, AQUA = "#2a78d6", "#eb6834", "#1baf7a"
RED = "#e34948"
SURFACE = "#fcfcfb"
GRID = "#e1e0d9"
INK = "#0b0b0b"
MUTED = "#898781"
SERIES = [BLUE, ORANGE, AQUA]
BLUE_RAMP = LinearSegmentedColormap.from_list(
    "blue_ramp", ["#fcfcfb", "#cde2fb", "#3987e5", "#104281"])

# 中文字体：先注册字体文件，再按名称引用。
FONT_PATH = os.path.expanduser(
    "~/.local/share/fonts/NotoSansCJKsc-Regular.ttf")
fm.fontManager.addfont(FONT_PATH)
FONT = fm.FontProperties(fname=FONT_PATH)
plt.rcParams.update({
    "font.family": FONT.get_name(),
    "font.sans-serif": [FONT.get_name()],
    "axes.unicode_minus": False,
    "axes.edgecolor": GRID,
    "axes.labelcolor": INK,
    "text.color": INK,
    "xtick.color": MUTED,
    "ytick.color": MUTED,
    "axes.facecolor": SURFACE,
    "figure.facecolor": SURFACE,
    "savefig.facecolor": SURFACE,
    "axes.grid": True,
    "grid.color": GRID,
    "grid.linewidth": 0.8,
    "axes.spines.top": False,
    "axes.spines.right": False,
})


def load_rows():
    with open(RESULTS, encoding="utf-8") as f:
        rows = list(csv.DictReader(f))
    for r in rows:
        r["block"] = int(r["block"])
        r["volume_bytes"] = int(r["volume_bytes"])
        r["blockiness_gray"] = float(r["blockiness_gray"])
    return rows


def save(fig, name):
    os.makedirs(FIG, exist_ok=True)
    path = os.path.join(FIG, name)
    fig.savefig(path, dpi=150, bbox_inches="tight")
    plt.close(fig)
    print("写出", path)


def fig_overview(rows):
    images = sorted({r["image"] for r in rows})
    idx = {(r["image"], r["block"]): r for r in rows}
    labels = {r["image"]: r["label"] for r in rows}

    fig = plt.figure(figsize=(13, 10))
    gs = fig.add_gridspec(2, 2, height_ratios=[1, 1.05], hspace=0.32, wspace=0.22)
    axv = fig.add_subplot(gs[0, 0])
    axq = fig.add_subplot(gs[0, 1])
    axp = fig.add_subplot(gs[1, 0])
    axt = fig.add_subplot(gs[1, 1])
    axt.axis("off")

    # V(B) 与 Q(B)：两个独立坐标系，绝不共用双轴。
    for c, img in zip(SERIES, images):
        vs = [idx[(img, b)]["volume_bytes"] for b in BLOCKS]
        qs = [idx[(img, b)]["blockiness_gray"] for b in BLOCKS]
        axv.plot(BLOCKS, vs, "-o", color=c, lw=2, ms=8,
                 label=labels[img], zorder=3)
        axq.plot(BLOCKS, qs, "-o", color=c, lw=2, ms=8, zorder=3)
    # 三幅图的最低点都在 B=8，统一标一个共享标记，避免三个标注重叠。
    axv.axvline(8, color=MUTED, lw=1.2, ls=":", zorder=1)
    axv.annotate("三图最低点均为 B=8", xy=(8, 0.04), xycoords=("data", "axes fraction"),
                 ha="center", fontsize=9, color=MUTED, fontproperties=FONT)

    axv.set_title("体积函数 V(B)：先降后升（U 形）", fontproperties=FONT, fontsize=12)
    axv.set_xlabel("块长 B（像素）", fontproperties=FONT)
    axv.set_ylabel("编码体积（字节）", fontproperties=FONT)
    axv.set_xticks(BLOCKS)
    axv.legend(prop=FONT, frameon=False, fontsize=10)
    axq.set_title("方块感函数 Q(B)：严格单调上升", fontproperties=FONT, fontsize=12)
    axq.set_xlabel("块长 B（像素）", fontproperties=FONT)
    axq.set_ylabel("方块感（灰度级）", fontproperties=FONT)
    axq.set_xticks(BLOCKS)

    # Pareto 散点：三幅图各自的 (Q, V)，前沿点实心、B=16 空心红圈。
    # 每个 (图像, 块) 组合给独立标签偏移，避免同档三点的标签互相压字。
    label_xy = {
        2:  [(-2, 12), (10, 8), (-2, -14)],
        4:  [(9, 9), (12, 0), (9, -11)],
        8:  [(-2, 10), (10, 2), (10, -11)],
        16: [(9, 8), (9, 0), (9, -11)],
    }
    for c, img in zip(SERIES, images):
        img_rank = SERIES.index(c)
        for b in BLOCKS:
            r = idx[(img, b)]
            on_front = b in FRONT
            axp.scatter(r["blockiness_gray"], r["volume_bytes"],
                        s=110, color=c if on_front else "white",
                        edgecolors=c if on_front else RED,
                        linewidths=2 if on_front else 2.2, zorder=3)
            dx, dy = label_xy[b][img_rank]
            axp.annotate(f"B={b}", (r["blockiness_gray"], r["volume_bytes"]),
                         textcoords="offset points", xytext=(dx, dy),
                         fontsize=8.5, color=INK, fontproperties=FONT,
                         ha="left" if dx > 0 else "right")
    axp.set_title("Pareto 前沿：实心点 {2,4,8} 非支配，空心红圈 B=16 被 B=8 支配",
                  fontproperties=FONT, fontsize=11.5)
    axp.set_xlabel("方块感 Q（灰度级，越小越好）", fontproperties=FONT)
    axp.set_ylabel("体积 V（字节，越小越好）", fontproperties=FONT)

    # 宽表：四档各占一行。
    col_labels = ["块长 B"]
    for img in images:
        col_labels += [f"{labels[img]}\nV/字节", f"{labels[img]}\nQ/灰度级"]
    cell = []
    for b in BLOCKS:
        line = [str(b)]
        for img in images:
            r = idx[(img, b)]
            line += [f"{r['volume_bytes']:,}", f"{r['blockiness_gray']:.3f}"]
        cell.append(line)
    tbl = axt.table(cellText=cell, colLabels=col_labels, loc="center",
                    cellLoc="center")
    tbl.auto_set_font_size(False)
    tbl.set_fontsize(9.5)
    tbl.scale(1, 1.7)
    for (row, col), obj in tbl.get_celld().items():
        obj.set_edgecolor(GRID)
        obj.set_facecolor("white" if row else "#eef3fb")
        obj.get_text().set_fontproperties(FONT)
        if row == 4:  # B=16 行：被支配，淡红底
            obj.set_facecolor("#fdecec")
    axt.set_title("四档数据宽表（B=16 行标红：被 B=8 双指标支配）",
                  fontproperties=FONT, fontsize=11.5, pad=14)

    fig.suptitle("块长—体积—方块感：函数关系与 Pareto 前沿",
                 fontproperties=FONT, fontsize=15, y=0.995)
    save(fig, "fig0_overview.png")


def boundary_map(base: np.ndarray, b: int) -> np.ndarray:
    """逐边界段平均绝对跳变：每个跨块像素对的 |灰度差|，非边界处置 0。"""
    h, w = base.shape
    nbh, nbw = h // b, w // b
    d = base.astype(np.int16)
    jump = np.zeros((h, w), dtype=np.float64)
    for kb in range(b, nbw * b, b):
        j = np.abs(d[:, kb - 1] - d[:, kb])
        jump[:, kb - 1] += j / 2.0
        jump[:, kb] += j / 2.0
    for kb in range(b, nbh * b, b):
        j = np.abs(d[kb - 1, :] - d[kb, :])
        jump[kb - 1, :] += j / 2.0
        jump[kb, :] += j / 2.0
    return jump


def fig_compare(b: int):
    # 统一选分块几何图的固定 64x64 裁剪窗，跨圆形边缘，边缘能量最强。
    img = read_pgm(os.path.join(ART, "images", "img2_shapes.pgm"))
    y0, x0, side = 104, 112, 64
    crop = img[y0:y0 + side, x0:x0 + side]
    grid = block_means(img, b)
    base_full = reconstruct(grid, b)
    base = base_full[y0:y0 + side, x0:x0 + side]
    jmap = boundary_map(base_full, b)[y0:y0 + side, x0:x0 + side]
    q, qv, qh = blockiness(base_full.astype(np.uint8), b)

    fig, axes = plt.subplots(1, 3, figsize=(13.5, 4.9))
    im0 = axes[0].imshow(crop, cmap="gray", vmin=0, vmax=255)
    axes[0].set_title("原图裁剪（64×64，固定窗）", fontproperties=FONT, fontsize=12)

    axes[1].imshow(base, cmap="gray", vmin=0, vmax=255)
    # 块网格：白线 + 深色描边，保证在任何灰度上可见。
    for k in range(b, side, b):
        axes[1].axvline(k - 0.5, color=INK, lw=2.2, alpha=0.55, zorder=3)
        axes[1].axvline(k - 0.5, color="white", lw=1.0, zorder=4)
        axes[1].axhline(k - 0.5, color=INK, lw=2.2, alpha=0.55, zorder=3)
        axes[1].axhline(k - 0.5, color="white", lw=1.0, zorder=4)
    axes[1].set_title(f"块均值重建 + 块网格（B={b}）",
                      fontproperties=FONT, fontsize=12)

    im2 = axes[2].imshow(jmap, cmap=BLUE_RAMP, vmin=0,
                         vmax=max(jmap.max(), 1))
    axes[2].set_title("边界两侧灰度跳变 |Δ|（灰度级）",
                      fontproperties=FONT, fontsize=12)
    cb = fig.colorbar(im2, ax=axes[2], fraction=0.046, pad=0.04)
    cb.set_label("跳变（灰度级）", fontproperties=FONT)

    for ax in axes:
        ax.set_xticks(np.arange(0, side + 1, 16))
        ax.set_yticks(np.arange(0, side + 1, 16))
        ax.tick_params(labelsize=8.5)
        ax.grid(False)

    fig.suptitle(
        f"块长 B={b} 对比 — 全图方块感一阶矩 Q={q:.3f} 灰度级"
        f"（垂直 {qv:.3f} / 水平 {qh:.3f}），跳变沿块边界集中即方块感",
        fontproperties=FONT, fontsize=13, y=1.02)
    save(fig, f"fig_compare_B{b}.png")


def main():
    rows = load_rows()
    fig_overview(rows)
    for b in FRONT:
        fig_compare(b)


if __name__ == "__main__":
    main()
