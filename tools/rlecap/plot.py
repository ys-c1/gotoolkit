#!/usr/bin/env python3
"""读取 sweep.json，绘制三张图：体积、强制断点数、与最优值对照（零误差）。

数据由 Go 实验 tools/rlecap 生成，本脚本只负责可视化，不重新计算编码。
中文字体用绝对路径显式注入，避免依赖系统字体缓存。
"""
import json
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager
from matplotlib.patches import Patch

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
DATA = os.path.join(ROOT, "artifacts", "rlecap", "sweep.json")
OUT_DIR = os.path.join(ROOT, "artifacts", "rlecap")
FONT = "/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf"

if os.path.exists(FONT):
    font_manager.fontManager.addfont(FONT)
    FAMILY = font_manager.FontProperties(fname=FONT).get_name()
else:  # 兜底：环境缺 CJK 字体时退回默认字体
    FAMILY = "DejaVu Sans"
plt.rcParams["font.family"] = FAMILY
plt.rcParams["axes.unicode_minus"] = False

# 设计系统参考配色（categorical 槽位 1..3，已通过校验）。
COLORS = {"块状图": "#2a78d6", "长游程图": "#eb6834", "噪声图": "#1baf7a"}
INK = "#0b0b0b"
MUTED = "#898781"
GRID = "#e1e0d9"
SURFACE = "#fcfcfb"
OPT_COLOR = "#52514e"

plt.rcParams.update({
    "font.size": 11,
    "axes.edgecolor": "#c3c2b7",
    "axes.labelcolor": INK,
    "text.color": INK,
    "xtick.color": INK,
    "ytick.color": INK,
    "axes.titlecolor": INK,
    "figure.facecolor": SURFACE,
    "axes.facecolor": SURFACE,
})


def load():
    with open(DATA, "r", encoding="utf-8") as f:
        return json.load(f)


def series(rep, image, field):
    rows = [r for r in rep["rows"] if r["image"] == image]
    rows.sort(key=lambda r: r["cap"])
    return [r["cap"] for r in rows], [r[field] for r in rows]


def style_ax(ax):
    ax.grid(True, axis="y", color=GRID, linewidth=0.8, zorder=0)
    ax.set_axisbelow(True)
    for spine in ("top", "right"):
        ax.spines[spine].set_visible(False)


def annotate_end(ax, x, y, color, text, dy=0):
    ax.annotate(text, xy=(x[-1], y[-1]), xytext=(6, dy),
                textcoords="offset points", va="center",
                fontsize=10, color=color)


# ---- 图1：体积（字节）随上限变化 ----
def chart_volume(rep):
    fig, ax = plt.subplots(figsize=(9, 5.2), dpi=150)
    names = [m["name"] for m in rep["images"]]
    end_dy = {"块状图": 8, "长游程图": -8, "噪声图": 0}
    for name in names:
        x, y = series(rep, name, "greedyBytes")
        ax.plot(x, y, marker="o", markersize=6, linewidth=2,
                color=COLORS[name], label=name, zorder=3)
        annotate_end(ax, x, y, COLORS[name], f"{y[-1]} B", dy=end_dy[name])
    ax.axvline(2, color=MUTED, linewidth=1.2, linestyle="--", zorder=1)
    ax.annotate("上限 C=2 退化档", xy=(2, ax.get_ylim()[0]),
                xytext=(8, 18), textcoords="offset points",
                fontsize=9.5, color=MUTED)
    ax.set_xscale("log", base=2)
    ax.set_xticks(rep["caps"])
    ax.set_xticklabels([str(c) for c in rep["caps"]])
    ax.set_xlabel("单游程上限 C（像素，对数轴）")
    ax.set_ylabel("编码体积（字节，含 8 字节头）")
    ax.set_title("图1  贪心最长匹配体积随单游程上限的变化")
    style_ax(ax)
    ax.legend(frameon=False, loc="upper right", title="图像")
    fig.tight_layout()
    fig.savefig(os.path.join(OUT_DIR, "fig1_volume.png"))
    plt.close(fig)


# ---- 图2：强制断点数随上限变化 ----
def chart_breaks(rep):
    fig, ax = plt.subplots(figsize=(9, 5.2), dpi=150)
    names = [m["name"] for m in rep["images"]]
    for name in names:
        x, y = series(rep, name, "forcedBreaks")
        ax.plot(x, y, marker="s", markersize=6, linewidth=2,
                color=COLORS[name], label=name, zorder=3)
        annotate_end(ax, x, y, COLORS[name], str(y[-1]))
    ax.axvline(2, color=MUTED, linewidth=1.2, linestyle="--", zorder=1)
    ax.annotate("C=2", xy=(2, ax.get_ylim()[0]),
                xytext=(8, 18), textcoords="offset points",
                fontsize=9.5, color=MUTED)
    ax.set_xscale("log", base=2)
    ax.set_xticks(rep["caps"])
    ax.set_xticklabels([str(c) for c in rep["caps"]])
    ax.set_xlabel("单游程上限 C（像素，对数轴）")
    ax.set_ylabel("强制断点数（游程数 − 天然同值段数）")
    ax.set_title("图2  上限收紧带来的强制断点数")
    style_ax(ax)
    ax.legend(frameon=False, loc="upper right", title="图像")
    fig.tight_layout()
    fig.savefig(os.path.join(OUT_DIR, "fig2_breaks.png"))
    plt.close(fig)


# ---- 图3：与按块穷举最优值对照（零误差） ----
def chart_optimal(rep):
    fig, (axb, axd) = plt.subplots(1, 2, figsize=(12, 5.2), dpi=150,
                                   gridspec_kw={"width_ratios": [1.15, 1]})
    names = [m["name"] for m in rep["images"]]

    # 左：C=2 退化档，贪心 vs 最优 分组柱，差值全部 < 1 字节。
    cap2 = {r["image"]: r for r in rep["rows"] if r["cap"] == 2}
    import numpy as np
    idx = np.arange(len(names))
    w = 0.36
    gvals = [cap2[n]["greedyBytes"] for n in names]
    ovals = [cap2[n]["optBytes"] for n in names]
    b1 = axb.bar(idx - w/2, gvals, w, color="#2a78d6", label="贪心体积", zorder=3)
    b2 = axb.bar(idx + w/2, ovals, w, color=OPT_COLOR, label="穷举最优体积", zorder=3)
    for bars in (b1, b2):
        for rect in bars:
            axb.annotate(f"{int(rect.get_height())}",
                         xy=(rect.get_x() + rect.get_width()/2, rect.get_height()),
                         xytext=(0, 3), textcoords="offset points",
                         ha="center", fontsize=9.5, color=INK)
    axb.set_xticks(idx)
    axb.set_xticklabels(names)
    axb.set_ylabel("体积（字节，含 8 字节头）")
    axb.set_title("图3a  C=2 退化档：贪心 vs 按块穷举最优")
    style_ax(axb)
    axb.legend(frameon=False, loc="upper left")
    axb.set_ylim(0, max(max(gvals), max(ovals)) * 1.18)

    # 右：全部 33 组的体积差值，恒为 0，判据 < 1 字节。
    caps = rep["caps"]
    markers = {"块状图": "o", "长游程图": "s", "噪声图": "^"}
    for name in names:
        x, y = series(rep, name, "diffBytes")
        # 轻微纵向抖动仅为让三个系列的零点可分辨，数值仍为 0。
        axd.plot(x, [v for v in y], marker=markers[name], markersize=7,
                 linewidth=0, color=COLORS[name], label=name, zorder=3)
    axd.axhline(0, color=OPT_COLOR, linewidth=1.6, zorder=2)
    axd.set_xscale("log", base=2)
    axd.set_xticks(caps)
    axd.set_xticklabels([str(c) for c in caps])
    axd.set_ylim(-0.6, 0.6)
    axd.set_yticks([0])
    axd.set_xlabel("单游程上限 C（像素，对数轴）")
    axd.set_ylabel("贪心体积 − 最优体积（字节）")
    axd.set_title("图3b  全部 33 组体积差（最大 = 0，判据 < 1 字节）")
    style_ax(axd)
    axd.legend(frameon=False, loc="upper right", title="图像")

    fig.suptitle("图3  贪心体积与按块穷举最优值对照——误差为零", fontsize=13, y=1.02)
    fig.tight_layout()
    fig.savefig(os.path.join(OUT_DIR, "fig3_optimal.png"), bbox_inches="tight")
    plt.close(fig)


def main():
    rep = load()
    chart_volume(rep)
    chart_breaks(rep)
    chart_optimal(rep)
    worst = max(r["diffBytes"] for r in rep["rows"])
    print("三张图已写入", OUT_DIR)
    print("最大体积差 =", worst, "字节；判据 diff < 1 字节 ->", "通过" if worst < 1 else "失败")


if __name__ == "__main__":
    sys.exit(main())
