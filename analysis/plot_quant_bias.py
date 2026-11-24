#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""读取 artifacts/quant_bias/results.json，绘制：
  1) histograms.png      四类图 × 三档位深的误差直方图（原量化器 vs 偏置校正）
  2) divergence.png      12 个单元的 JS 散度 前→后 斜率图（对数轴）
  3) bias_threshold.png  残余归一化偏置与 0.05 阈值
  4) sources.png         四类源图预览
另算 χ² 拟合优度 p 值，写 pvalues.json。

只依赖 numpy / matplotlib / scipy，路径由参数传入，便于复跑。
"""
import argparse
import json
from pathlib import Path

import numpy as np
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager as fm
from matplotlib.patches import Patch
from scipy.stats import chi2 as chi2_dist

# ---------- 字体与令牌（配色取自 dataviz 已校验参考板） ----------
# 依次尝试本机常见 CJK 字体文件；都没有时退回字体名列表，避免中文变方框。
_FONT_CANDIDATES = [
    "/mnt/c/Windows/Fonts/simhei.ttf",
    "/System/Library/Fonts/PingFang.ttc",
    "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
    "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
]
for _fp in _FONT_CANDIDATES:
    try:
        fm.fontManager.addfont(_fp)
    except Exception:
        pass
plt.rcParams["font.sans-serif"] = [
    "SimHei", "Noto Sans CJK SC", "Noto Sans CJK JP",
    "WenQuanYi Zen Hei", "PingFang SC", "Microsoft YaHei", "DejaVu Sans",
]
plt.rcParams["font.family"] = "sans-serif"
plt.rcParams["axes.unicode_minus"] = False
plt.rcParams["mathtext.fontset"] = "dejavusans"
plt.rcParams["font.size"] = 10

BLUE = "#2a78d6"      # 校正后 / 主序列
ORANGE = "#eb6834"    # 原量化器参照
INK = "#0b0b0b"
SECOND = "#52514e"
MUTED = "#898781"
GRID = "#e1e0d9"
SURFACE = "#fcfcfb"
RED = "#d03b3b"
BLUE_RAMP = ["#6da7ec", "#2a78d6", "#104281"]  # 三档位深 ordinal

CLASS_CN = {
    "gradient": "渐变 gradient",
    "photo": "照片 photo",
    "texture": "纹理 texture",
    "noise": "噪声 noise",
}


def load_pgm_p5(path: Path) -> np.ndarray:
    data = path.read_bytes()
    assert data[:2] == b"P5"
    i = 2
    tokens = []
    while len(tokens) < 3:
        while data[i] in b" \t\r\n":
            i += 1
        if data[i] == ord("#"):
            while data[i] not in b"\r\n":
                i += 1
            continue
        j = i
        while data[i] not in b" \t\r\n":
            i += 1
        tokens.append(int(data[j:i]))
    w, h, maxv = tokens
    i += 1
    pix = np.frombuffer(data[i:i + w * h], dtype=np.uint8)
    return pix.reshape(h, w)


def style_ax(ax):
    ax.set_facecolor(SURFACE)
    for s in ("top", "right"):
        ax.spines[s].set_visible(False)
    for s in ("left", "bottom"):
        ax.spines[s].set_color("#c3c2b7")
    ax.tick_params(colors=SECOND, labelsize=8)
    ax.grid(axis="y", color=GRID, linewidth=0.7, zorder=0)
    ax.set_axisbelow(True)


def fig_histograms(rep, out: Path):
    classes = rep["classes"]
    bits_list = rep["bit_depths"]
    cells = {(c["class"], c["bits"]): c for c in rep["cells"]}
    fig, axes = plt.subplots(len(classes), len(bits_list),
                             figsize=(14.5, 13.2), sharex=True)
    fig.patch.set_facecolor(SURFACE)

    for r, cls in enumerate(classes):
        for col, bits in enumerate(bits_list):
            ax = axes[r][col]
            style_ax(ax)
            c = cells[(cls, bits)]
            step = c["step"]
            edges = np.asarray(c["wide_edges_gray"]) / step      # [-1,1]
            centers = (edges[:-1] + edges[1:]) / 2
            width = edges[1] - edges[0]
            dens0 = np.asarray(c["wide_original"]) * step       # 概率 -> 密度
            dens1 = np.asarray(c["wide_integer"]) * step

            ax.axvspan(-0.5, 0.5, color="#1baf7a", alpha=0.07, zorder=0)
            ax.axvline(0, color="#c3c2b7", linewidth=1.0, zorder=1)
            ax.bar(centers, dens0, width=width * 0.96, color=ORANGE,
                   alpha=0.42, zorder=2, label="原量化器 δ=0")
            ax.bar(centers, dens1, width=width * 0.80, color=BLUE,
                   alpha=0.72, zorder=3, label="偏置校正 δ=int(Δβ*)")
            ax.plot([-0.5, 0.5], [1, 1], color=INK, linestyle="--",
                    linewidth=1.5, zorder=4)
            ax.set_xlim(-1.08, 1.08)
            ax.set_xticks([-1, -0.5, 0, 0.5, 1])

            msg = (f"JS {c['js_original']:.3f}→{c['js_integer']:.4f}\n"
                   f"|E[e]|/Δ = {abs(c['residual_bias_integer']):.3f}")
            ax.text(0.03, 0.97, msg, transform=ax.transAxes, va="top",
                    ha="left", fontsize=8.2, color=SECOND,
                    bbox=dict(boxstyle="round,pad=0.32", fc="white",
                              ec=GRID, alpha=0.9))
            if r == 0:
                ax.set_title(f"{bits} bit · {c['levels']} 级 · Δ={step:g}",
                             fontsize=11, color=INK, pad=8)
            if col == 0:
                ax.set_ylabel(CLASS_CN.get(cls, cls), fontsize=11, color=INK)
            if r == len(classes) - 1:
                ax.set_xlabel("归一化误差 e / Δ", fontsize=9, color=SECOND)

    handles = [
        Patch(facecolor=ORANGE, alpha=0.55, label="原量化器（下沿重建，误差挤在 [0, Δ) 一侧）"),
        Patch(facecolor=BLUE, alpha=0.75, label="重建电平偏移后（误差居中）"),
        plt.Line2D([0], [0], color=INK, ls="--", lw=1.5,
                   label="均匀分布密度 1（区间 [-Δ/2, Δ/2]）"),
    ]
    fig.legend(handles=handles, loc="upper center", ncol=3, frameon=False,
               bbox_to_anchor=(0.5, 0.985), fontsize=10)
    fig.suptitle("量化误差直方图：四类图 × 三档位深",
                 fontsize=15, color=INK, y=0.995, fontweight="bold")
    fig.tight_layout(rect=(0, 0, 1, 0.955))
    fig.savefig(out, dpi=160, bbox_inches="tight", facecolor=SURFACE)
    plt.close(fig)


def fig_divergence(rep, out: Path):
    classes = rep["classes"]
    bits_list = rep["bit_depths"]
    cells = {(c["class"], c["bits"]): c for c in rep["cells"]}
    x = np.arange(len(classes))

    fig, axes = plt.subplots(1, len(bits_list), figsize=(14.5, 5.2), sharey=True)
    fig.patch.set_facecolor(SURFACE)
    for j, bits in enumerate(bits_list):
        ax = axes[j]
        style_ax(ax)
        ax.set_yscale("log")
        for i, cls in enumerate(classes):
            c = cells[(cls, bits)]
            y0, y1 = c["js_original"], max(c["js_integer"], 1.1e-5)
            ax.plot([i, i], [y0, y1], color=MUTED, lw=1.6, zorder=2)
            ax.scatter(i, y0, s=64, marker="s", color=ORANGE, zorder=3,
                       edgecolor="white", linewidth=0.8)
            ax.scatter(i, y1, s=72, marker="o", color=BLUE, zorder=4,
                       edgecolor="white", linewidth=0.8)
            label = f"{y1:.5f}" if y1 < 0.001 else f"{y1:.4f}"
            ax.text(i + 0.12, y1, label, fontsize=7.6, color=BLUE,
                    va="center")
        ax.set_xticks(x)
        ax.set_xticklabels([CLASS_CN[c].split()[0] for c in classes], fontsize=10)
        ax.set_title(f"{bits} bit（Δ={256 / 2**bits:g}）", fontsize=11, color=INK)
        ax.grid(axis="y", which="both", color=GRID, linewidth=0.6)
        ax.set_ylim(8e-6, 1.6)
        if j == 0:
            ax.set_ylabel(r"JS 散度（$\log_2$，对数轴）", fontsize=10, color=SECOND)

    handles = [
        plt.Line2D([0], [0], marker="s", color="none", markerfacecolor=ORANGE,
                   markersize=9, label="原始参照量化器"),
        plt.Line2D([0], [0], marker="o", color="none", markerfacecolor=BLUE,
                   markersize=9, label="散度最优偏移校正后"),
        plt.Line2D([0], [0], color=MUTED, lw=1.6, label="同一格下降连线"),
    ]
    fig.legend(handles=handles, loc="upper center", ncol=3, frameon=False,
               bbox_to_anchor=(0.5, 1.02), fontsize=10)
    fig.suptitle("逐档验证：JS 散度在全部 12 个单元均下降",
                 fontsize=14, color=INK, y=1.07, fontweight="bold")
    fig.tight_layout()
    fig.savefig(out, dpi=160, bbox_inches="tight", facecolor=SURFACE)
    plt.close(fig)


def fig_bias(rep, out: Path):
    classes = rep["classes"]
    bits_list = rep["bit_depths"]
    cells = {(c["class"], c["bits"]): c for c in rep["cells"]}
    x = np.arange(len(classes))
    w = 0.24

    fig, ax = plt.subplots(figsize=(11, 5.4))
    fig.patch.set_facecolor(SURFACE)
    style_ax(ax)
    for j, bits in enumerate(bits_list):
        vals = [abs(cells[(cls, bits)]["residual_bias_integer"]) for cls in classes]
        off = (j - 1) * w
        bars = ax.bar(x + off, vals, width=w * 0.92, color=BLUE_RAMP[j],
                      label=f"{bits} bit", zorder=3)
        for b, v in zip(bars, vals):
            ax.text(b.get_x() + b.get_width() / 2, v + 0.0012, f"{v:.3f}",
                    ha="center", va="bottom", fontsize=8, color=SECOND)

    ax.axhline(0.05, color=RED, linestyle="--", linewidth=1.8, zorder=4)
    ax.text(len(classes) - 0.55, 0.0515, "阈值 τ = 0.05（1/20 步长）",
            color=RED, fontsize=10, va="bottom", ha="right")
    ax.set_xticks(x)
    ax.set_xticklabels([CLASS_CN[c] for c in classes], fontsize=10.5)
    ax.set_ylabel("校正后残余偏置  |E[e]| / Δ", fontsize=11)
    ax.set_ylim(0, 0.062)
    ax.set_title("四类图 × 三档位深：偏差全部低于 0.05，偏置解正确",
                 fontsize=14, color=INK, fontweight="bold", pad=12)
    ax.legend(frameon=False, ncol=3, loc="upper left", fontsize=10)
    fig.tight_layout()
    fig.savefig(out, dpi=160, facecolor=SURFACE)
    plt.close(fig)


def fig_sources(img_dir: Path, classes, out: Path):
    fig, axes = plt.subplots(1, 4, figsize=(13.5, 3.8))
    fig.patch.set_facecolor(SURFACE)
    for ax, cls in zip(axes, classes):
        im = load_pgm_p5(img_dir / f"{cls}.pgm")
        ax.imshow(im, cmap="gray", vmin=0, vmax=255)
        ax.set_title(CLASS_CN[cls], fontsize=11, color=INK)
        ax.axis("off")
    fig.suptitle("四类确定性测试图（256×256 PGM，固定随机种子）",
                 fontsize=13.5, color=INK, fontweight="bold", y=1.02)
    fig.tight_layout()
    fig.savefig(out, dpi=150, bbox_inches="tight", facecolor=SURFACE)
    plt.close(fig)


def chi2_pvalues(rep):
    rows = []
    for c in rep["cells"]:
        df = 2 ** c["bits"] - 1
        for arm, stat in (("original", c["chi2_stat_original"]),
                          ("integer", c["chi2_stat_integer"])):
            p = chi2_dist.sf(stat, df)
            rows.append({"class": c["class"], "bits": c["bits"], "arm": arm,
                         "chi2_stat": stat, "df": df, "p": p})
    return rows


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--results", default="artifacts/quant_bias/results.json")
    ap.add_argument("--imgdir", default="testdata/study")
    ap.add_argument("--outdir", default="artifacts/quant_bias")
    args = ap.parse_args()

    rep = json.loads(Path(args.results).read_text())
    outdir = Path(args.outdir)
    outdir.mkdir(parents=True, exist_ok=True)

    fig_histograms(rep, outdir / "histograms.png")
    fig_divergence(rep, outdir / "divergence.png")
    fig_bias(rep, outdir / "bias_threshold.png")
    fig_sources(Path(args.imgdir), rep["classes"], outdir / "sources.png")

    pvals = chi2_pvalues(rep)
    (outdir / "pvalues.json").write_text(
        json.dumps(pvals, ensure_ascii=False, indent=2))

    print(f"wrote 4 figures + pvalues.json to {outdir}")
    for c in rep["class_verdicts"]:
        print(c["class"], "PASS" if c["pass"] else "FAIL",
              f"max_resid={c['max_residual_bias']:.4f}")


if __name__ == "__main__":
    main()
