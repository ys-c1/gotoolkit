#!/usr/bin/env python3 -s
"""抖动矩阵周期 -> 主频 -> 可见度 正式实验。

产物 (全部写入 analysis/results/):
  data/measurements.csv      3 周期 × 2 距离的完整测量表 (主频与可见度同行)
  data/results.json          关键数值汇总 (供报告引用)
  patterns/pattern_T*.png    三档周期的图案放大图 (各一张)
  patterns/fullframe_T*.pgm  1024² 完整 P5 图 (供仓库 Go 解码器验证)
  figures/fig1_spectrum.*    径向谱 + f*=1/T 反比验证
  figures/fig2_csf_band.*    CSF 与敏感带、三档主频落点
  figures/fig3_threshold.*   阈值周期与观察距离的线性关系
  figures/fig4_visibility.*  可见度与 CSF 阈值逐档比对 (差为 margin 一个数)
  figures/fig5_retinal.*     两档距离下的视网膜成像放大对照
"""
from __future__ import annotations

import csv
import json
import os
import sys

import numpy as np

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import dither_core as dc  # noqa: E402

import matplotlib  # noqa: E402
matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib import font_manager  # noqa: E402
from PIL import Image  # noqa: E402

# 注册系统里的 Noto Sans CJK (matplotlib 缓存可能未收录)
for _cand in (
    os.path.expanduser("~/.local/share/fonts/NotoSansCJKsc-Regular.ttf"),
    "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
):
    if os.path.exists(_cand):
        font_manager.fontManager.addfont(_cand)
        CJK_FONT = font_manager.FontProperties(fname=_cand).get_name()
        break
else:
    CJK_FONT = "DejaVu Sans"

# ---------------------------------------------------------------- 实验常量
PERIODS = [4, 16, 64]                 # 三档矩阵周期 (像素)
DISTANCES_M = [0.4, 4.0]              # 两档观察距离 (米)
N = 1024                              # 抖动画幅
GRAY = 0.5                            # 均匀输入灰阶
COLORS = {4: "#2a78d6", 16: "#eb6834", 64: "#1baf7a"}   # dataviz 槽位 1/2/3
INK = "#0b0b0b"
INK2 = "#52514e"
MUTED = "#898781"
GRID = "#e1e0d9"
SURFACE = "#fcfcfb"
THRESH_COLOR = "#e34948"

ROOT = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(ROOT, "results")
PAT_DIR = os.path.join(OUT, "patterns")
FIG_DIR = os.path.join(OUT, "figures")
DAT_DIR = os.path.join(OUT, "data")
for d in (PAT_DIR, FIG_DIR, DAT_DIR):
    os.makedirs(d, exist_ok=True)

plt.rcParams.update({
    "font.family": CJK_FONT,
    "font.size": 11,
    "axes.unicode_minus": False,
    "text.color": INK,
    "axes.edgecolor": "#c3c2b7",
    "axes.labelcolor": INK,
    "axes.titlecolor": INK,
    "xtick.color": INK2,
    "ytick.color": INK2,
    "axes.grid": True,
    "grid.color": GRID,
    "grid.linewidth": 0.8,
    "axes.facecolor": SURFACE,
    "figure.facecolor": SURFACE,
    "savefig.facecolor": SURFACE,
    "axes.spines.top": False,
    "axes.spines.right": False,
})


# ================================================================ 测量
def measure_all() -> list[dict]:
    rows = []
    for T in PERIODS:
        for D in DISTANCES_M:
            rows.append(dc.measure_row(T, D, N=N, gray=GRAY))
    return rows


def write_csv(rows: list[dict]) -> None:
    path = os.path.join(DAT_DIR, "measurements.csv")
    cols = list(rows[0].keys())
    with open(path, "w", newline="", encoding="utf-8") as fh:
        w = csv.DictWriter(fh, fieldnames=cols)
        w.writeheader()
        for r in rows:
            w.writerow(r)
    print("wrote", path)


# ================================================================ 图案
def save_patterns() -> None:
    crop_px = 128                  # 三张图统一裁剪 128×128 物理像素, 保证同尺度可比
    zoom = 4                       # 统一 4 倍最近邻放大 -> 512×512
    for T in PERIODS:
        img = dc.dither_raster(T, gray=GRAY, N=N)
        crop = img[:crop_px, :crop_px]
        big = np.kron(crop, np.ones((zoom, zoom)))
        png = (big * 255).astype(np.uint8)
        p = os.path.join(PAT_DIR, f"pattern_T{T:02d}.png")
        Image.fromarray(png, mode="L").save(p)
        print("wrote", p, f"(crop {crop_px}px = {crop_px / T:.0f} 个周期, x{zoom})")

        # 完整 P5 PGM
        pgm_path = os.path.join(PAT_DIR, f"fullframe_T{T:02d}.pgm")
        with open(pgm_path, "wb") as fh:
            fh.write(f"P5\n{N} {N}\n255\n".encode("ascii"))
            fh.write((img * 255).astype(np.uint8).tobytes())
        print("wrote", pgm_path)


# ================================================================ 图 0: 汇总表 (主频与可见度同行)
def fig0_table(rows: list[dict]) -> None:
    header = ["周期 T\n(px)", "1/T\n(cy/px)", "主频 f*\n(cy/px)", "距离 D\n(m)",
              "角频率\n(cpd)", "可见度 V\n(整图 RMS)", "阈值 C_thr", "Δ=V−C_thr", "判定"]
    cell_text, cell_colors = [], []
    for r in rows:
        verdict = "可见" if r["margin_V_minus_thr"] > 0 else "不可见"
        cell_text.append([
            f"{r['T_px']}",
            f"{1.0 / r['T_px']:.5f}",
            f"{r['f_pix_cyc_per_px']:.5f}",
            f"{r['D_m']:.1f}",
            f"{r['f_ang_cpd']:.2f}",
            f"{r['visibility_rms']:.4f}",
            f"{r['csf_threshold']:.4f}",
            f"{r['margin_V_minus_thr']:+.4f}",
            verdict,
        ])
        row_col = [SURFACE] * 9
        row_col[8] = "#e8f5ee" if r["margin_V_minus_thr"] > 0 else "#fdeaea"
        cell_colors.append(row_col)

    fig, ax = plt.subplots(figsize=(12.5, 3.4))
    ax.axis("off")
    tbl = ax.table(cellText=cell_text, colLabels=header, cellColours=cell_colors,
                   colColours=["#f0efec"] * 9, loc="center", cellLoc="center")
    tbl.auto_set_font_size(False)
    tbl.set_fontsize(10.5)
    tbl.scale(1, 1.7)
    for (row, col), cell in tbl.get_celld().items():
        cell.set_edgecolor("#e1e0d9")
        if row == 0:
            cell.set_text_props(color=INK, weight="bold")
        elif col in (0, 2, 5, 7):
            cell.set_text_props(color=INK)
        else:
            cell.set_text_props(color=INK2)
    ax.set_title("主频与可见度同行汇总: T 增大 → f* 反比减小; Δ>0 为落在可见区",
                 fontsize=12.5, pad=12)
    fig.tight_layout()
    p = os.path.join(FIG_DIR, "fig0_summary_table.png")
    fig.savefig(p, dpi=150, bbox_inches="tight")
    plt.close(fig)
    print("wrote", p)


# ================================================================ 图 1: 频谱与反比验证
def fig1_spectrum() -> None:
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11.5, 4.6))

    measured = {}
    for T in PERIODS:
        img = dc.dither_raster(T, gray=GRAY, N=N)
        centers, radial, _, _ = dc.fft_spectrum(img)
        f_star = dc.dominant_frequency_fine(img)
        measured[T] = f_star
        ax1.plot(centers, radial / radial.max(), color=COLORS[T], lw=1.8,
                 label=f"T = {T} px")
        ax1.axvline(1.0 / T, color=COLORS[T], ls=":", lw=1.2, alpha=0.7)
        ax1.annotate(f"f*={f_star:.4f}", xy=(1.0 / T, 1.0),
                     xytext=(4, -14 - 12 * PERIODS.index(T)),
                     textcoords="offset points", color=COLORS[T], fontsize=9.5)

    ax1.set_xlim(0, 0.30)
    ax1.set_xlabel("空间频率 f (cycles/pixel)")
    ax1.set_ylabel("归一化径向幅度谱")
    ax1.set_title("(a) 三档周期的径向幅度谱: 主峰随周期左移")
    ax1.legend(frameon=False, loc="upper right")

    inv_T = np.array([1.0 / T for T in PERIODS])
    f_meas = np.array([measured[T] for T in PERIODS])
    ax2.plot(inv_T, f_meas, "o", ms=9, color="#2a78d6", mec=SURFACE, mew=1.5, zorder=3)
    lo, hi = 0.0, max(inv_T) * 1.12
    ax2.plot([lo, hi], [lo, hi], color=MUTED, ls="--", lw=1.4, label="f* = 1/T")
    for T, x, y in zip(PERIODS, inv_T, f_meas):
        ax2.annotate(f"T={T}", xy=(x, y), xytext=(7, 6), textcoords="offset points",
                     color=INK2, fontsize=9.5)
    err = np.max(np.abs(f_meas - inv_T) / inv_T) * 100
    ax2.set_xlim(lo, hi)
    ax2.set_ylim(lo, hi)
    ax2.set_xlabel("1/T (cycles/pixel)")
    ax2.set_ylabel("实测主频 f* (cycles/pixel)")
    ax2.set_title(f"(b) 实测主频 vs 1/T (16× 零填充 FFT, 最大偏差 {err:.1f}%)")
    ax2.legend(frameon=False, loc="upper left")

    fig.tight_layout()
    p = os.path.join(FIG_DIR, "fig1_spectrum.png")
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print("wrote", p)
    return measured, err


# ================================================================ 图 2: CSF 与敏感带
def fig2_csf_band(rows: list[dict], f_lo: float, f_hi: float) -> None:
    fig, ax = plt.subplots(figsize=(9.5, 5.0))
    f = np.geomspace(0.05, 100, 600)
    S = dc.csf_sensitivity(f)
    ax.plot(f, S, color=INK, lw=2.0, label="CSF: S(f)=Smax·x·exp(1−x), x=f/f0")
    ax.axhline(dc.S_BAND_MIN, color=THRESH_COLOR, ls="--", lw=1.4,
               label=f"敏感带判据 S = {dc.S_BAND_MIN:.0f} (C_thr = 0.05)")
    ax.axvspan(f_lo, f_hi, color="#2a78d6", alpha=0.10,
               label=f"视觉敏感带 {f_lo:.2f}–{f_hi:.1f} cpd")

    # 三档周期在两档距离下的主频落点 (取 4m 为主标注, 0.4m 用空心点)
    label_offsets = {4: (-12, 22), 16: (10, 14), 64: (-118, 8)}
    for T in PERIODS:
        for D, marker, fill in [(0.4, "o", "none"), (4.0, "o", COLORS[T])]:
            r = next(x for x in rows if x["T_px"] == T and abs(x["D_m"] - D) < 1e-9)
            fa = r["f_ang_cpd"]
            s_at = float(dc.csf_sensitivity(np.array([fa]))[0])
            ax.plot(fa, max(s_at, 0.8), marker, ms=10, mfc=fill,
                    mec=COLORS[T], mew=2, zorder=4)
        r4 = next(x for x in rows if x["T_px"] == T and abs(x["D_m"] - 4.0) < 1e-9)
        s4 = float(dc.csf_sensitivity(np.array([r4["f_ang_cpd"]]))[0])
        dx, dy = label_offsets[T]
        ax.annotate(f"T={T}, 4m: {r4['f_ang_cpd']:.1f} cpd",
                    xy=(r4["f_ang_cpd"], max(s4, 0.8)),
                    xytext=(dx, dy), textcoords="offset points",
                    color=COLORS[T], fontsize=9.5)

    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.set_xlim(0.05, 100)
    ax.set_ylim(0.7, 300)
    ax.set_xlabel("视网膜角频率 f_ang (cycles/degree)")
    ax.set_ylabel("对比敏感度 S")
    ax.set_title("CSF 敏感带与三档周期主频的落点 (实心=4m, 空心=0.4m)")
    ax.legend(frameon=False, loc="lower center", fontsize=9.5)
    fig.tight_layout()
    p = os.path.join(FIG_DIR, "fig2_csf_band.png")
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print("wrote", p)


# ================================================================ 图 3: 阈值周期与距离
def fig3_threshold(f_lo: float, f_hi: float) -> None:
    fig, ax = plt.subplots(figsize=(9.5, 5.2))
    Ds = np.linspace(0.2, 5.0, 200)
    T_lo_edge = np.array([dc.threshold_period(D, f_lo) for D in Ds])
    T_hi_edge = np.array([dc.threshold_period(D, f_hi) for D in Ds])
    ax.plot(Ds, T_lo_edge, color=THRESH_COLOR, lw=2.0,
            label=f"低频边界 T*_lo = ppd(D)/{f_lo:.2f}")
    ax.plot(Ds, T_hi_edge, color="#6250d6", lw=2.0,
            label=f"高频边界 T*_hi = ppd(D)/{f_hi:.1f}")
    ax.fill_between(Ds, T_hi_edge, T_lo_edge, color="#1baf7a", alpha=0.10,
                    label="可见窗口 (主频落在敏感带)")
    k_lo = dc.TAN1 / dc.PIXEL_PITCH_M / f_lo
    k_hi = dc.TAN1 / dc.PIXEL_PITCH_M / f_hi
    ax.text(0.78, 1180, f"T*_lo = {k_lo:.1f}·D (px), 与距离严格线性",
            color=THRESH_COLOR, fontsize=10)
    ax.text(3.05, 95, f"T*_hi = {k_hi:.2f}·D (px)", color="#6250d6", fontsize=10)

    # 三条实验周期水平线 (线性轴上挤在底部), 用引线分层标注
    t_label_cfg = {4: (1.35, 235), 16: (1.35, 150), 64: (0.55, 60)}
    for T in PERIODS:
        ax.axhline(T, color=COLORS[T], ls=":", lw=1.3)
        lx, ly = t_label_cfg[T]
        ax.annotate(f"实验周期 T={T}", xy=(0.02, T), xytext=(lx, ly),
                    color=COLORS[T], fontsize=9.5,
                    arrowprops=dict(arrowstyle="-", color=COLORS[T], lw=0.9,
                                    shrinkA=0, shrinkB=2))

    # 两档观察距离竖线, 标注贴竖线、放图内上部
    for D in DISTANCES_M:
        ax.axvline(D, color=MUTED, ls="--", lw=1.0)
        ax.annotate(f"D={D} m", xy=(D + 0.04, 1280), ha="left", va="top",
                    color=INK2, fontsize=9.5)

    ax.set_xlim(0, 5.0)
    ax.set_ylim(0, 1500)
    ax.set_xlabel("观察距离 D (m)")
    ax.set_ylabel("阈值周期 T* (像素)")
    ax.set_title("敏感带阈值周期随观察距离线性变化 (像素节距 0.265 mm, 96 dpi)")
    ax.legend(frameon=False, loc="center right", fontsize=9.5)
    fig.tight_layout()
    p = os.path.join(FIG_DIR, "fig3_threshold.png")
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print("wrote", p)


# ================================================================ 图 4: 可见度逐档比对
def fig4_visibility(rows: list[dict]) -> None:
    fig, axes = plt.subplots(1, 2, figsize=(11.5, 4.8), sharey=True)
    for ax, D in zip(axes, DISTANCES_M):
        sub = [r for r in rows if abs(r["D_m"] - D) < 1e-9]
        x = np.arange(len(PERIODS))
        w = 0.36
        V = [r["visibility_rms"] for r in sub]
        C = [r["csf_threshold"] for r in sub]
        b1 = ax.bar(x - w / 2, V, w, color="#2a78d6", label="可见度 V (视网膜整图 RMS 对比度)")
        b2 = ax.bar(x + w / 2, C, w, color=THRESH_COLOR, label="CSF 阈值对比度 C_thr(f*)")
        for i, r in enumerate(sub):
            m = r["margin_V_minus_thr"]
            verdict = "可见" if m > 0 else "不可见"
            ax.annotate(f"Δ={m:+.3f}\n{verdict}", xy=(x[i], max(V[i], C[i])),
                        xytext=(0, 6), textcoords="offset points", ha="center",
                        fontsize=9.5, color=INK)
        ax.set_xticks(x)
        ax.set_xticklabels([f"T={T}" for T in PERIODS])
        ax.set_title(f"观察距离 D = {D} m")
        ax.set_ylim(0, 1.15)
        ax.axhline(0, color="#c3c2b7", lw=1)
    axes[0].set_ylabel("对比度 (无量纲)")
    axes[0].legend(frameon=False, loc="upper right", fontsize=9.0)
    fig.suptitle("可见度指标与主频同行比对: Δ = V − C_thr(f*), Δ>0 可见", y=1.02)
    fig.tight_layout()
    p = os.path.join(FIG_DIR, "fig4_visibility.png")
    fig.savefig(p, dpi=150, bbox_inches="tight")
    plt.close(fig)
    print("wrote", p)


# ================================================================ 图 5: 视网膜成像对照
def fig5_retinal() -> None:
    fig, axes = plt.subplots(2, 3, figsize=(11.0, 7.4))
    for j, T in enumerate(PERIODS):
        img = dc.dither_raster(T, gray=GRAY, N=N)
        for i, D in enumerate(DISTANCES_M):
            ret = dc.retinal_image(img, D)
            side = 8 * T
            crop = ret[:side, :side]
            vmin, vmax = -0.55, 0.55
            axes[i, j].imshow(crop, cmap="gray", vmin=vmin, vmax=vmax, interpolation="nearest")
            axes[i, j].set_xticks([])
            axes[i, j].set_yticks([])
            r = dc.measure_row(T, D, N=N, gray=GRAY)
            tag = "可见" if r["margin_V_minus_thr"] > 0 else "不可见"
            axes[i, j].set_title(f"T={T}, D={D}m  f={r['f_ang_cpd']:.1f} cpd  "
                                 f"V={r['visibility_rms']:.3f} ({tag})", fontsize=10)
    axes[0, 0].set_ylabel("D = 0.4 m", fontsize=11)
    axes[1, 0].set_ylabel("D = 4.0 m", fontsize=11)
    fig.suptitle("眼睛光学 MTF 后的视网膜成像 (同尺度 8×8 周期局部, 对称灰度轴)", y=1.0)
    fig.tight_layout()
    p = os.path.join(FIG_DIR, "fig5_retinal.png")
    fig.savefig(p, dpi=150, bbox_inches="tight")
    plt.close(fig)
    print("wrote", p)


# ================================================================ JSON 汇总
def write_json(rows, measured, err, f_lo, f_hi) -> None:
    summary = {
        "config": {
            "periods_px": PERIODS,
            "distances_m": DISTANCES_M,
            "grid_N": N,
            "gray": GRAY,
            "pixel_pitch_m": dc.PIXEL_PITCH_M,
            "pupil_m": dc.PUPIL_M,
            "wavelength_m": dc.WAVELENGTH_M,
            "csf": {"Smax": dc.S_MAX, "f0_cpd": dc.F0_CPD, "form": "Smax*x*exp(1-x)"},
            "sensitive_band_rule": f"S >= {dc.S_BAND_MIN}",
        },
        "equation_check": {
            "measured_f_star": {str(T): measured[T] for T in PERIODS},
            "one_over_T": {str(T): 1.0 / T for T in PERIODS},
            "max_relative_error_pct": err,
        },
        "sensitive_band_cpd": {"low": f_lo, "high": f_hi},
        "threshold_periods_px": {
            f"D={D}": {
                "ppd": dc.ppd(D),
                "T_star_low_edge": dc.threshold_period(D, f_lo),
                "T_star_high_edge": dc.threshold_period(D, f_hi),
            } for D in DISTANCES_M
        },
        "measurements": rows,
    }
    p = os.path.join(DAT_DIR, "results.json")

    def _default(o):
        if isinstance(o, np.generic):
            return o.item()
        raise TypeError(f"不可序列化: {type(o)}")

    with open(p, "w", encoding="utf-8") as fh:
        json.dump(summary, fh, ensure_ascii=False, indent=2, default=_default)
    print("wrote", p)


def main() -> None:
    f_lo, f_hi = dc.sensitive_band()
    rows = measure_all()
    write_csv(rows)
    save_patterns()
    fig0_table(rows)
    measured, err = fig1_spectrum()
    fig2_csf_band(rows, f_lo, f_hi)
    fig3_threshold(f_lo, f_hi)
    fig4_visibility(rows)
    fig5_retinal()
    write_json(rows, measured, err, f_lo, f_hi)


if __name__ == "__main__":
    main()
