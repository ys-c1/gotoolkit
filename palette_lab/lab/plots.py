"""读取 data/metrics.json 与 alabama.json, 生成四张图到 figures/。

图 1 prediction.png   实测 SSE 点 + 逐格模型预测曲线(主) + 两条闭式参考曲线
图 2 tradeoff.png     五档覆盖率与 RMS ΔE 双面板(两列)
图 3 baselines.png    贪心(pv) / 纯 p / Hamilton 三条 SSE 曲线对比
图 4 allocation.png   K=256 下每格桶数: pv 贪心 vs Hamilton, 按权重排序
"""

from __future__ import annotations

import json
import os
import sys

# -I 隔离模式不读 PYTHONPATH/用户 site, 显式加入绘图依赖目录
for _p in (
    "/home/ys/.local/lib/python3.14/site-packages",
    "/usr/local/lib/python3.14/dist-packages",
):
    if os.path.isdir(_p) and _p not in sys.path:
        sys.path.append(_p)

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA = os.path.join(ROOT, "data")
FIG = os.path.join(ROOT, "figures")

# dataviz 参考调色板(light)
BLUE, ORANGE, AQUA, YELLOW, MAGENTA, VIOLET = (
    "#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4", "#6250d6",
)
INK, MUTED, GRID, SURFACE = "#0b0b0b", "#52514e", "#e3e2dd", "#fcfcfb"

plt.rcParams.update(
    {
        "font.size": 10,
        "axes.edgecolor": "#8a8985",
        "axes.labelcolor": INK,
        "axes.titlecolor": INK,
        "xtick.color": MUTED,
        "ytick.color": MUTED,
        "axes.grid": True,
        "grid.color": GRID,
        "grid.linewidth": 0.8,
        "axes.axisbelow": True,
        "figure.facecolor": SURFACE,
        "axes.facecolor": SURFACE,
        "savefig.facecolor": SURFACE,
    }
)


def load():
    with open(os.path.join(DATA, "metrics.json")) as f:
        m = json.load(f)
    with open(os.path.join(DATA, "alabama.json")) as f:
        al = json.load(f)
    return m, al


def fig_prediction(m):
    ks = np.array(m["K_levels"], dtype=float)
    sse = np.array([r["sse"] for r in m["weighted"]])
    pred = np.array([r["pred_sse"] for r in m["weighted"]])
    closed = np.array([r["closed_sse"] for r in m["weighted"]])
    curve = np.array([r["curve_sse"] for r in m["weighted"]])

    fig, ax = plt.subplots(figsize=(7.6, 4.8))
    # 细网格 K 画成平滑预测曲线: 五档之间在对数空间线性插值后取指数
    kk = np.geomspace(ks.min(), ks.max(), 80)
    smooth = lambda vals: np.exp(np.interp(np.log(kk), np.log(ks), np.log(vals)))
    ax.plot(kk, smooth(pred),
            color=BLUE, lw=2, label="Predicted SSE  (per-cell power law, calibrated)")
    ax.plot(kk, smooth(closed),
            color=AQUA, lw=1.6, ls="--",
            label="w/n continuous relaxation  (1/K, anchor at K=64)")
    ax.plot(kk, smooth(curve),
            color=YELLOW, lw=1.8, ls=":",
            label="Effective exponent curve  (r_eff from K=64,128)")
    ax.scatter(ks, sse, s=64, color=ORANGE, zorder=5, edgecolor="white",
               linewidth=1.2, label="Measured SSE  (actual k-means quantization)")
    # 残差标注统一放数据点下方, 避开图例
    for x, y, r in zip(ks, sse, m["weighted"]):
        ax.annotate(f"{r['pred_resid_pct']:+.1f}%", (x, y),
                    textcoords="offset points", xytext=(0, -15),
                    ha="center", fontsize=8.5, color=MUTED)
    ax.set_xscale("log", base=2)
    ax.set_yscale("log")
    ax.set_xticks(ks)
    ax.set_xticklabels([str(int(k)) for k in ks])
    ax.set_xlabel("Palette buckets K")
    ax.set_ylabel("Total squared color error (SSE)")
    ax.set_title("Measured points land on the allocation-predicted curve")
    ax.legend(frameon=False, fontsize=8.6, loc="lower left")
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "prediction.png"), dpi=160)
    plt.close(fig)


def fig_tradeoff(m):
    ks = [str(r["K"]) for r in m["weighted"]]
    cover = [100 * r["coverage"] for r in m["weighted"]]
    rms = [r["rms_de"] for r in m["weighted"]]
    sat = m["saturation_K"]

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(10.4, 4.2))
    x = np.arange(len(ks))
    b1 = ax1.bar(x, cover, color=BLUE, width=0.62, zorder=3)
    ax1.set_ylim(0, 105)
    ax1.set_xticks(x)
    ax1.set_xticklabels(ks)
    ax1.set_xlabel("Palette buckets K")
    ax1.set_ylabel("Coverage @ |ΔE| ≤ 16  (%)")
    ax1.set_title("Coverage")
    for rect, v in zip(b1, cover):
        ax1.text(rect.get_x() + rect.get_width() / 2, v + 1.5,
                 f"{v:.1f}", ha="center", fontsize=9, color=INK)
    if sat is not None:
        si = ks.index(str(sat))
        ax1.axvline(si, color=MAGENTA, lw=1.4, ls="--")
        ax1.annotate(f"saturation\nK={sat}", (si, 55), color=MAGENTA,
                     fontsize=9, ha="center")

    b2 = ax2.bar(x, rms, color=ORANGE, width=0.62, zorder=3)
    ax2.set_xticks(x)
    ax2.set_xticklabels(ks)
    ax2.set_xlabel("Palette buckets K")
    ax2.set_ylabel("RMS color error  |ΔE|")
    ax2.set_title("RMS color error")
    for rect, v in zip(b2, rms):
        ax2.text(rect.get_x() + rect.get_width() / 2, v + 0.3,
                 f"{v:.2f}", ha="center", fontsize=9, color=INK)
    fig.suptitle("Five bucket levels: coverage and color error", fontsize=11)
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "tradeoff.png"), dpi=160)
    plt.close(fig)


def fig_baselines(m):
    ks = np.array(m["K_levels"], dtype=float)
    sse_w = np.array([r["sse"] for r in m["weighted"]])
    sse_p = np.array([r["sse"] for r in m["p_only"]])
    sse_h = np.array([r["sse"] for r in m["naive"]])

    fig, ax = plt.subplots(figsize=(7.6, 4.6))
    ax.plot(ks, sse_h, color=MAGENTA, lw=2, marker="o", ms=7,
            label="Hamilton proportional (largest remainder)")
    ax.plot(ks, sse_p, color=AQUA, lw=2, marker="s", ms=7,
            label="Greedy on pixel share p only (ignores volume)")
    ax.plot(ks, sse_w, color=BLUE, lw=2.2, marker="D", ms=7,
            label="Greedy on w = p·v  (proposed)")
    ax.set_xscale("log", base=2)
    ax.set_yscale("log")
    ax.set_xticks(ks)
    ax.set_xticklabels([str(int(k)) for k in ks])
    ax.set_xlabel("Palette buckets K")
    ax.set_ylabel("Total squared color error (SSE)")
    ax.set_title("Allocation rule matters more than bucket count")
    ax.legend(frameon=False, fontsize=9)
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "baselines.png"), dpi=160)
    plt.close(fig)


def fig_allocation(m):
    K = 256
    ki = m["K_levels"].index(K)
    g_alloc = np.array(m["allocations"]["greedy"][str(K)])
    h_alloc = np.array(m["allocations"]["hamilton"][str(K)])
    stats = m["cell_stats"]
    order = np.argsort([c["w"] for c in stats])[::-1]
    g_alloc = g_alloc[order]
    h_alloc = h_alloc[order]
    labels = [str(stats[i]["idx"]) for i in order]

    x = np.arange(len(labels))
    fig, ax = plt.subplots(figsize=(11.5, 4.4))
    ax.bar(x - 0.2, g_alloc, width=0.4, color=BLUE, zorder=3,
           label="Greedy w=p·v")
    ax.bar(x + 0.2, h_alloc, width=0.4, color=MAGENTA, zorder=3,
           label="Hamilton proportional")
    ax.set_xticks(x)
    ax.set_xticklabels(labels, fontsize=7.5, rotation=90)
    ax.set_xlabel("Gamut cell index (sorted by weight w, high → low)")
    ax.set_ylabel(f"Buckets assigned (K={K})")
    ax.set_title("Bucket distribution across gamut cells at K=256")
    ax.legend(frameon=False, fontsize=9)
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "allocation.png"), dpi=160)
    plt.close(fig)


def fig_alabama(al):
    # 用最小反例画 K 14..17 每格桶数, 高亮丢桶格
    mc = al["minimal_counterexample"]
    rows = mc["allocations"]
    ks = [r["K"] for r in rows]
    h = np.array([r["hamilton"] for r in rows])
    g = np.array([r["greedy"] for r in rows])
    c = h.shape[1]
    lost = mc["event"]["cell"]

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(10.4, 4.0), sharey=True)
    im1 = ax1.imshow(h, aspect="auto", cmap="Blues")
    im2 = ax2.imshow(g, aspect="auto", cmap="Greens")
    for ax, mat, name in ((ax1, h, "Hamilton"), (ax2, g, "Greedy w=p·v")):
        ax.set_xticks(range(c))
        ax.set_xticklabels([f"cell {i}" for i in range(c)], fontsize=8.5)
        ax.set_yticks(range(len(ks)))
        ax.set_yticklabels([f"K={k}" for k in ks])
        ax.set_title(name)
        for i in range(mat.shape[0]):
            for j in range(mat.shape[1]):
                ax.text(j, i, str(mat[i, j]), ha="center", va="center",
                        fontsize=9, color=INK)
        ax.grid(False)
    ax1.add_patch(plt.Rectangle((lost - 0.5, 1.5), 1, 1, fill=False,
                                edgecolor=ORANGE, lw=2.5))
    ax1.annotate("lost bucket", (lost, 1), xytext=(lost + 0.9, 0.4),
                 color=ORANGE, fontsize=9,
                 arrowprops=dict(arrowstyle="->", color=ORANGE, lw=1.2))
    fig.colorbar(im1, ax=ax1, fraction=0.046, pad=0.03)
    fig.colorbar(im2, ax=ax2, fraction=0.046, pad=0.03)
    fig.suptitle("Alabama paradox: adding one bucket, Hamilton takes one away",
                 fontsize=11)
    fig.tight_layout()
    fig.savefig(os.path.join(FIG, "alabama.png"), dpi=160)
    plt.close(fig)


def main():
    os.makedirs(FIG, exist_ok=True)
    m, al = load()
    fig_prediction(m)
    fig_tradeoff(m)
    fig_baselines(m)
    fig_allocation(m)
    fig_alabama(al)
    print("figures written to", FIG)
    for f in sorted(os.listdir(FIG)):
        print(" ", f)


if __name__ == "__main__":
    main()
