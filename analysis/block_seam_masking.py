#!/usr/bin/env python3
"""Controlled texture-masking experiment for a fixed block seam.

The script creates three widely separated local-variance regimes, inserts the
same 8-bit gray-level seam into each, calibrates a variance-dependent
visibility threshold, and writes tables, charts, and subjective comparison
figures under artifacts/block_seam_masking.
"""

from __future__ import annotations

import argparse
import json
import math
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd
from matplotlib import font_manager as fm
from scipy.ndimage import gaussian_filter
from scipy.stats import norm, spearmanr

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "artifacts" / "block_seam_masking"

# Palette: first three validated categorical slots from the dataviz reference.
INK = "#0b0b0b"
INK_2 = "#52514e"
MUTED = "#898781"
GRID = "#e1e0d9"
SURFACE = "#fcfcfb"
BLUE = "#2a78d6"
ORANGE = "#eb6834"
AQUA = "#1baf7a"
RED = "#d03b3b"
GREEN = "#0ca30c"

FONT_PATH = Path("/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf")
if FONT_PATH.exists():
    fm.fontManager.addfont(str(FONT_PATH))
    FONT_NAME = fm.FontProperties(fname=str(FONT_PATH)).get_name()
    plt.rcParams["font.family"] = FONT_NAME
plt.rcParams["axes.unicode_minus"] = False
plt.rcParams["figure.facecolor"] = "white"
plt.rcParams["axes.facecolor"] = SURFACE
plt.rcParams["axes.edgecolor"] = "#c3c2b7"
plt.rcParams["axes.labelcolor"] = INK
plt.rcParams["xtick.color"] = INK_2
plt.rcParams["ytick.color"] = INK_2
plt.rcParams["text.color"] = INK

SEED = 20261126
SIZE = 128
SEAM_X = SIZE // 2  # Right column of the boundary: boundary is at SEAM_X - 0.5.
BLOCK = 8
Y0 = SIZE // 2 - BLOCK // 2
MV_Y0 = 48
TEXTURE_CORR = 1.0

QUANT_STEP = 16.0  # gray levels; representative seam is q/2.
SEAM_AMP = QUANT_STEP / 2.0
FLAT_THRESHOLD = 2.0  # gray levels at zero added texture variance.
P_TARGET = 0.75
KAPPA = float(np.sqrt(2.0) * norm.ppf(P_TARGET))  # 2AFC 75% coefficient.
SIGMA_D2 = (FLAT_THRESHOLD / KAPPA) ** 2

CLASS_DEFS = [
    {
        "key": "flat",
        "name": "平坦区",
        "target_var": 2.25,
        "color": BLUE,
        "expectation": "缝应明显",
    },
    {
        "key": "medium",
        "name": "中度纹理区",
        "target_var": 256.0,
        "color": ORANGE,
        "expectation": "接近阈值",
    },
    {
        "key": "strong",
        "name": "强纹理区",
        "target_var": 1600.0,
        "color": AQUA,
        "expectation": "缝被掩蔽",
    },
]

V_GRID = np.array(
    [1.0, 2.25, 4.0, 9.0, 16.0, 36.0, 64.0, 128.0, 256.0, 400.0, 550.0, 800.0, 1200.0, 1600.0],
    dtype=float,
)
AMP_GRID = np.array([0.0, 1.0, 2.0, 3.0, 4.0, 6.0, 8.0, 10.0, 14.0, 18.0, 24.0])


def make_texture(shape: int | tuple[int, int], target_var: float, rng: np.random.Generator) -> np.ndarray:
    """Generate a stationary, spatially correlated texture with target variance."""
    if isinstance(shape, int):
        shape = (shape, shape)
    z = rng.standard_normal(shape)
    z = gaussian_filter(z, sigma=TEXTURE_CORR, mode="reflect")
    z = (z - z.mean()) / z.std()
    x = 128.0 + np.sqrt(target_var) * z
    return np.clip(np.rint(x), 0, 255).astype(np.uint8)


def add_seam(img: np.ndarray, amp: float = SEAM_AMP) -> np.ndarray:
    """Shift the left block side down and right side up by amp/2."""
    x = img.astype(np.float64)
    out = x.copy()
    out[:, :SEAM_X] -= amp / 2.0
    out[:, SEAM_X:] += amp / 2.0
    return np.clip(np.rint(out), 0, 255).astype(np.uint8)


def seam_response(img: np.ndarray, x0: int = SEAM_X, y0: int = Y0, block: int = BLOCK) -> float:
    """Mean signed one-pixel cross-boundary jump over one 8-pixel block segment."""
    x = img.astype(np.float64)
    return float(np.mean(x[y0 : y0 + block, x0] - x[y0 : y0 + block, x0 - 1]))


def local_variance(img: np.ndarray) -> float:
    """Pooled variance in two 32x32 side windows, excluding the seam columns."""
    x = img.astype(np.float64)
    left = x[MV_Y0 : MV_Y0 + 32, SEAM_X - 34 : SEAM_X - 2]
    right = x[MV_Y0 : MV_Y0 + 32, SEAM_X + 2 : SEAM_X + 34]
    left = left - left.mean()
    right = right - right.mean()
    residual = np.concatenate([left.ravel(), right.ravel()])
    return float(np.mean(residual**2))


def calibrate(n_per_var: int) -> tuple[pd.DataFrame, dict[float, np.ndarray], float, np.ndarray]:
    """Estimate Var(seam statistic) = c * local variance by Monte Carlo."""
    rng = np.random.default_rng(SEED)
    # A 48x64 stationary patch is large enough for sigma=1 correlation and the
    # same centered 8-pixel by one-pixel seam statistic used on 128px figures.
    h, w = 48, 64
    x0, y0 = w // 2, h // 2 - BLOCK // 2
    samples: dict[float, np.ndarray] = {}
    rows = []
    for v in V_GRID:
        d = np.empty(n_per_var, dtype=np.float64)
        for i in range(n_per_var):
            tex = make_texture((h, w), float(v), rng)
            d[i] = seam_response(tex, x0=x0, y0=y0)
        samples[float(v)] = d
        var_d = float(np.var(d, ddof=1))
        t_emp = KAPPA * np.sqrt(var_d + SIGMA_D2)
        rows.append(
            {
                "variance": float(v),
                "response_variance": var_d,
                "response_sd": float(np.sqrt(var_d)),
                "threshold_amp": float(t_emp),
                "n": n_per_var,
            }
        )
    cal = pd.DataFrame(rows)

    # Relative sampling variance of a sample variance is approximately
    # 2 sigma^4/(n-1), so inverse-relative-variance weighting is 1/var_d^2.
    sigma = cal["response_variance"].to_numpy() * np.sqrt(2.0 / max(n_per_var - 1, 1))

    def model(v: np.ndarray, c: float) -> np.ndarray:
        return c * v

    # A tiny grid search plus weighted least squares keeps this dependency-light.
    c_grid = np.linspace(0.10, 0.24, 1401)
    wgt = 1.0 / np.maximum(sigma**2, 1e-12)
    ss = [np.sum(wgt * (cal["response_variance"].to_numpy() - c * V_GRID) ** 2) for c in c_grid]
    c0 = float(c_grid[int(np.argmin(ss))])
    denom = float(np.sum(wgt * V_GRID**2))
    numer = float(np.sum(wgt * V_GRID * cal["response_variance"].to_numpy()))
    c = numer / denom if denom > 0 else c0

    boot_rng = np.random.default_rng(SEED + 17)
    boot_vc = []
    fit_vs = V_GRID[V_GRID >= 4.0]
    for _ in range(500):
        ratios = []
        for v in fit_vs:
            d = samples[float(v)]
            idx = boot_rng.integers(0, len(d), len(d))
            ratios.append(np.var(d[idx], ddof=1) / v)
        c_b = float(np.mean(ratios))
        boot_vc.append((SEAM_AMP**2 - FLAT_THRESHOLD**2) / (KAPPA**2 * c_b))
    ci = np.percentile(np.asarray(boot_vc), [2.5, 97.5])
    return cal, samples, c, ci


def choose_exemplar(target_var: float, class_index: int, c: float) -> tuple[np.ndarray, float, float]:
    """Pick a variance-representative patch without an accidental natural edge."""
    rng = np.random.default_rng(SEED + 101 + class_index)
    best: tuple[float, np.ndarray, float, float] | None = None
    expected_sd = np.sqrt(SIGMA_D2 + c * target_var)
    for _ in range(401):
        img = make_texture(SIZE, target_var, rng)
        v = local_variance(img)
        natural_jump = seam_response(img)
        var_error = abs(np.log(max(v, 1e-9) / target_var))
        # Standardize the accidental boundary alignment so all classes use the
        # same rule. This isolates variance masking from a chance texture edge.
        edge_penalty = 0.35 * (natural_jump / expected_sd) ** 2
        score = var_error + edge_penalty
        if best is None or score < best[0]:
            best = (score, img, v, natural_jump)
    assert best is not None
    return best[1], best[2], best[3]


def af_threshold(v: float, c: float) -> float:
    return float(np.sqrt(FLAT_THRESHOLD**2 + KAPPA**2 * c * max(v, 0.0)))


def critical_variance(c: float) -> float:
    return float((SEAM_AMP**2 - FLAT_THRESHOLD**2) / (KAPPA**2 * c))


def monte_carlo_detection(d_clean: np.ndarray, v: float, rng: np.random.Generator) -> tuple[np.ndarray, float]:
    """2AFC probabilities using independent texture draws in the two intervals."""
    n = len(d_clean)
    perm = rng.permutation(n)
    eps_yes = rng.normal(0.0, np.sqrt(SIGMA_D2), size=n)
    eps_no = rng.normal(0.0, np.sqrt(SIGMA_D2), size=n)
    probs = []
    for amp in AMP_GRID:
        decision_diff = amp + d_clean + eps_yes - d_clean[perm] - eps_no
        probs.append(float(np.mean(decision_diff > 0.0)))
    p8 = probs[list(AMP_GRID).index(SEAM_AMP)]
    return np.asarray(probs), p8


def verdict_for(v_ratio: float) -> tuple[str, str]:
    if v_ratio >= 1.10:
        return "可见", RED
    if v_ratio > 0.90:
        return "临界", "#eda100"
    return "被掩蔽", GREEN


def plot_masking_curve(cal: pd.DataFrame, c: float, vc: float, ci: np.ndarray, class_rows: list[dict]) -> Path:
    fig, ax = plt.subplots(figsize=(9.4, 6.2), constrained_layout=True)
    v_line = np.geomspace(0.8, 1800.0, 300)
    t_line = np.array([af_threshold(float(v), c) for v in v_line])

    below = t_line <= SEAM_AMP
    ax.fill_between(
        v_line,
        t_line,
        SEAM_AMP,
        where=below,
        interpolate=True,
        color=RED,
        alpha=0.12,
        linewidth=0,
    )
    ax.fill_between(
        v_line,
        SEAM_AMP,
        t_line,
        where=~below,
        interpolate=True,
        color=GREEN,
        alpha=0.12,
        linewidth=0,
    )
    ax.plot(v_line, t_line, color=BLUE, lw=2.6, label="拟合阈值 $T(\\sigma_L^2)$")
    ax.scatter(
        cal["variance"],
        cal["threshold_amp"],
        s=42,
        color=BLUE,
        edgecolor="white",
        linewidth=0.8,
        zorder=3,
        label="Monte Carlo 标定",
    )
    ax.axhline(SEAM_AMP, color=ORANGE, lw=2.3, label=f"同一条块缝 $A={SEAM_AMP:g}$ GL ($q={QUANT_STEP:g}$)")
    ax.axvline(vc, color=INK, lw=1.8, ls="--", label=f"临界方差 $\\sigma_c^2={vc:.0f}$ GL²")
    ax.scatter([vc], [SEAM_AMP], s=70, color=INK, zorder=5, edgecolor="white", linewidth=1.0)

    for row in class_rows:
        ax.scatter(
            [row["v_meas"]],
            [row["a_threshold"]],
            s=118,
            marker="D",
            color=row["color"],
            edgecolor="white",
            linewidth=1.2,
            zorder=6,
        )
        dy = 1.15 if row["key"] != "medium" else -1.55
        ax.annotate(
            f"{row['name']}\n{row['v_meas']:.0f} GL², V={row['visibility']:.2f}",
            xy=(row["v_meas"], row["a_threshold"]),
            xytext=(0, 18 if dy > 0 else -34),
            textcoords="offset points",
            ha="center",
            fontsize=9.5,
            color=INK,
            arrowprops={"arrowstyle": "-", "color": MUTED, "lw": 0.8},
        )

    ax.text(18.0, 10.1, "缝高 > 阈值：可见", color="#7a2020", fontsize=11.5, weight="bold")
    ax.text(vc * 1.16, 1.05, "阈值 > 缝高：掩蔽", color="#075c07", fontsize=11.5, weight="bold")
    ax.set_xscale("log")
    ax.set_xlim(0.8, 2200)
    ax.set_ylim(0, 21)
    ax.set_xlabel("局部方差 $\\sigma_L^2$（灰度平方 GL²，对数轴）", fontsize=11.5)
    ax.set_ylabel("75% 2AFC 缝高阈值 $T$（灰度级 GL）", fontsize=11.5)
    ax.set_title("纹理方差对块缝的掩蔽：阈值随方差单调上升", fontsize=14, weight="bold", pad=12)
    ax.grid(True, which="major", color=GRID, lw=0.8)
    ax.grid(True, which="minor", axis="x", color=GRID, lw=0.45, alpha=0.65)
    ax.spines[["top", "right"]].set_visible(False)
    ax.legend(loc="upper left", frameon=True, framealpha=0.94, facecolor="white", edgecolor=GRID, fontsize=9.5)
    fig.text(
        0.99,
        0.01,
        f"bootstrap 95% CI for σc²: [{ci[0]:.0f}, {ci[1]:.0f}] GL²",
        ha="right",
        fontsize=8.5,
        color=MUTED,
    )
    path = OUT / "masking_curve.png"
    fig.savefig(path, dpi=180, bbox_inches="tight")
    plt.close(fig)
    return path


def plot_psychometric(class_rows: list[dict]) -> Path:
    fig, ax = plt.subplots(figsize=(8.8, 5.8), constrained_layout=True)
    amp_line = np.linspace(0, 24, 240)
    ax.axhline(P_TARGET, color=MUTED, ls="--", lw=1.4, label=f"75% 正确判据")
    for row in class_rows:
        s_tot = np.sqrt(SIGMA_D2 + row["c"] * row["v_meas"])
        p_line = norm.cdf(amp_line / np.sqrt(2.0 * s_tot**2))
        ax.plot(amp_line, p_line, color=row["color"], lw=2.4)
        ax.scatter(
            AMP_GRID,
            row["p_mc_curve"],
            s=34,
            color=row["color"],
            edgecolor="white",
            linewidth=0.7,
            zorder=4,
        )
        ax.scatter([SEAM_AMP], [row["p_mc8"]], s=92, marker="o", color=row["color"], edgecolor=INK, linewidth=0.7, zorder=6)
        ax.annotate(
            f"{row['name']}\nT={row['a_threshold']:.1f} GL",
            xy=(row["a_threshold"], P_TARGET),
            xytext=(8, -30 if row["key"] != "strong" else 18),
            textcoords="offset points",
            fontsize=9.5,
            arrowprops={"arrowstyle": "->", "color": MUTED, "lw": 0.9},
        )
    ax.set_xlim(0, 24)
    ax.set_ylim(0.48, 1.01)
    ax.set_xlabel("插入缝高 $A$（灰度级 GL）")
    ax.set_ylabel("2AFC 正确率")
    ax.set_title("同一判据下，纹理越强，心理测量曲线越向右移", fontsize=13.5, weight="bold")
    ax.grid(True, color=GRID, lw=0.8)
    ax.spines[["top", "right"]].set_visible(False)
    handles = [plt.Line2D([0], [0], color=r["color"], lw=3, label=r["name"]) for r in class_rows]
    handles.append(plt.Line2D([0], [0], color=MUTED, ls="--", label="75% 阈值"))
    ax.legend(handles=handles, loc="lower right", frameon=True, facecolor="white", edgecolor=GRID)
    path = OUT / "psychometric_curves.png"
    fig.savefig(path, dpi=180, bbox_inches="tight")
    plt.close(fig)
    return path


def plot_subjective(row: dict) -> Path:
    clean = row["clean"]
    seamed = row["seamed"]
    v = row["v_meas"]
    vc = row["vc"]
    visibility = row["visibility"]
    verdict, verdict_color = verdict_for(visibility)

    fig = plt.figure(figsize=(16.0, 9.4))
    gs = fig.add_gridspec(
        2,
        4,
        left=0.045,
        right=0.985,
        bottom=0.075,
        top=0.86,
        wspace=0.20,
        hspace=0.30,
        width_ratios=[1.0, 1.0, 1.0, 1.12],
    )
    ax0 = fig.add_subplot(gs[0, 0])
    ax1 = fig.add_subplot(gs[0, 1])
    axz = fig.add_subplot(gs[0, 2])
    axd = fig.add_subplot(gs[0, 3])
    axp = fig.add_subplot(gs[1, 0:2])
    axt = fig.add_subplot(gs[1, 2:4])

    edge = SEAM_X - 0.5
    for ax, img, title in [(ax0, clean, "无块缝对照"), (ax1, seamed, f"插入同一条缝 $A={SEAM_AMP:g}$ GL")]:
        ax.imshow(img, cmap="gray", vmin=0, vmax=255)
        ax.set_title(title, fontsize=12, weight="bold")
        ax.set_xticks([])
        ax.set_yticks([])
        ax.scatter([edge], [-4], marker="v", s=55, color=row["color"], clip_on=False, zorder=5)
        ax.scatter([edge], [SIZE + 3], marker="^", s=55, color=row["color"], clip_on=False, zorder=5)

    half = 16
    crop = seamed[48:80, SEAM_X - half : SEAM_X + half]
    axz.imshow(crop, cmap="gray", vmin=0, vmax=255, interpolation="nearest")
    axz.set_title("缝周 32×32 放大", fontsize=12, weight="bold")
    axz.set_xticks([])
    axz.set_yticks([])
    crop_edge = half - 0.5
    # Edge markers sit outside the pixels; do not draw a cue line through the crop.
    axz.scatter([crop_edge], [-1.7], marker="v", s=70, color=row["color"], clip_on=False, zorder=5)
    axz.scatter([crop_edge], [33.7], marker="^", s=70, color=row["color"], clip_on=False, zorder=5)

    diff = seamed.astype(np.float64) - clean.astype(np.float64)
    imd = axd.imshow(diff, cmap="gray", vmin=-8, vmax=8)
    axd.set_title("差值图（色标 ±8 GL）", fontsize=12, weight="bold")
    axd.set_xticks([])
    axd.set_yticks([])
    cbar = fig.colorbar(imd, ax=axd, fraction=0.046, pad=0.04, ticks=[-8, -4, 0, 4, 8])
    cbar.set_label("seamed − clean（GL）")

    xs = np.arange(SEAM_X - 14, SEAM_X + 14)
    p_clean = clean[Y0 : Y0 + BLOCK, :][:, xs].mean(axis=0)
    p_seam = seamed[Y0 : Y0 + BLOCK, :][:, xs].mean(axis=0)
    axp.plot(xs, p_clean, color=MUTED, lw=2.0, label="无块缝")
    axp.plot(xs, p_seam, color=row["color"], lw=2.5, label="有块缝")
    axp.axvline(edge, color=INK_2, ls="--", lw=1.1)
    axp.set_title(f"沿缝 {BLOCK} 像素平均后的横向灰度剖面", fontsize=12, weight="bold")
    axp.set_xlabel("横坐标 x（像素）")
    axp.set_ylabel("平均灰度（GL）")
    axp.grid(True, color=GRID, lw=0.8)
    axp.spines[["top", "right"]].set_visible(False)
    axp.legend(frameon=True, facecolor="white", edgecolor=GRID, fontsize=9.5)

    axt.axis("off")
    delta_v = v - vc
    lines = [
        f"区域：{row['name']}（目标方差 {row['target_var']:.2f} GL²）",
        f"实测局部方差 σL² = {v:.2f} GL²",
        f"临界方差 σc² = {vc:.2f} GL²",
        f"方差差 σL² − σc² = {delta_v:+.2f} GL²",
        f"实测缝跳变 Â = {row['a_meas']:.2f} GL；局部阈值 T = {row['a_threshold']:.2f} GL",
        f"实测归一化可见度 V = Â/T = {visibility:.3f}；V−1 = {visibility - 1:+.3f}",
        f"d′ = {row['dprime']:.3f}；2AFC 正确率 Monte Carlo = {row['p_mc8']:.3f}",
        f"判定（V=1 为 75% 阈值）：{verdict}",
    ]
    axt.text(0.01, 0.96, "\n".join(lines), va="top", ha="left", fontsize=12.2, linespacing=1.85)
    axt.add_patch(plt.Rectangle((0.01, 0.08), 0.98, 0.18, transform=axt.transAxes,
                                facecolor=verdict_color, alpha=0.12, edgecolor=verdict_color, lw=1.5))
    axt.text(0.50, 0.17, verdict, transform=axt.transAxes, ha="center", va="center",
             fontsize=20, weight="bold", color=verdict_color)

    fig.text(
        0.5,
        0.965,
        f"{row['name']}主观对照：实测方差 {v:.1f} GL²，临界方差 {vc:.1f} GL²",
        ha="center",
        va="top",
        fontsize=18,
        weight="bold",
    )
    fig.text(
        0.5,
        0.925,
        f"同一条块缝：量化步长 q={QUANT_STEP:g} GL，缝高 A={SEAM_AMP:g} GL；归一化可见度 V={visibility:.2f}，判定为「{verdict}」",
        ha="center",
        va="top",
        fontsize=12.5,
        color=INK_2,
    )
    path = OUT / f"subjective_{row['key']}.png"
    fig.savefig(path, dpi=175, bbox_inches="tight")
    plt.close(fig)
    return path


def dataframe_to_markdown(df: pd.DataFrame) -> str:
    """Small dependency-free GitHub-flavored markdown table writer."""
    cols = [str(c) for c in df.columns]
    lines = ["| " + " | ".join(cols) + " |", "| " + " | ".join(["---"] * len(cols)) + " |"]
    for _, row in df.iterrows():
        lines.append("| " + " | ".join(str(row[c]) for c in df.columns) + " |")
    return "\n".join(lines)


def write_markdown_report(
    cal: pd.DataFrame,
    c: float,
    ci: np.ndarray,
    vc: float,
    rows: list[dict],
    rho: float,
    p_exact: float,
    monotonic: bool,
    n_cal: int,
) -> Path:
    table_df = pd.DataFrame(
        [
            {
                "区域": r["name"],
                "实测方差 (GL²)": round(r["v_meas"], 2),
                "临界方差 (GL²)": round(r["vc"], 2),
                "方差差 σ²−σc² (GL²)": round(r["v_meas"] - r["vc"], 2),
                "实测缝高 (GL)": round(r["a_meas"], 2),
                "阈值 T(σ²) (GL)": round(r["a_threshold"], 2),
                "可见度 V": round(r["visibility"], 3),
                "V−1": round(r["visibility"] - 1, 3),
                "2AFC正确率": round(r["p_mc8"], 3),
                "判定": verdict_for(r["visibility"])[0],
            }
            for r in rows
        ]
    )
    csv_path = OUT / "region_table.csv"
    table_df.to_csv(csv_path, index=False, encoding="utf-8-sig")
    table_md = dataframe_to_markdown(table_df)

    sensitivity = []
    for a0 in [1.0, 2.0, 3.0]:
        sensitivity.append(
            {
                "flat_threshold_gl": a0,
                "critical_variance_gl2": float((SEAM_AMP**2 - a0**2) / (KAPPA**2 * c)),
            }
        )

    payload = {
        "constants": {
            "quant_step_gl": QUANT_STEP,
            "seam_amplitude_gl": SEAM_AMP,
            "flat_threshold_gl": FLAT_THRESHOLD,
            "target_2afc": P_TARGET,
            "kappa": KAPPA,
            "sigma_decision_squared_gl2": SIGMA_D2,
            "texture_correlation_sigma_px": TEXTURE_CORR,
            "block_aperture_px": BLOCK,
        },
        "c_dimensionless": c,
        "critical_variance_gl2": vc,
        "critical_variance_bootstrap95_gl2": [float(ci[0]), float(ci[1])],
        "spearman_threshold_variance": {
            "rho": float(rho),
            "exact_two_sided_permutation_p": float(p_exact),
            "n_grid": int(len(V_GRID)),
            "empirical_monotonic": bool(monotonic),
        },
        "sensitivity_to_flat_threshold": sensitivity,
        "classes": [
            {k: (v if not isinstance(v, np.ndarray) else v.tolist()) for k, v in r.items() if k not in {"clean", "seamed", "p_mc_curve", "color"}}
            for r in rows
        ],
    }
    (OUT / "metrics.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")

    md = f"""# 块缝的局部方差掩蔽实验

## 结论

- 固定同一条缝：量化步长 \\(q={QUANT_STEP:g}\\) GL，取代表性缝高 \\(A=q/2={SEAM_AMP:g}\\) GL。
- 75% 二选一强制选择（2AFC）判据下，可见阈值为

\\[
T(\\sigma_L^2)=\\sqrt{{A_0^2+\\kappa^2 c\\sigma_L^2}},
\\quad A_0={FLAT_THRESHOLD:g}\\ \\mathrm{{GL}},\\quad
\\kappa=\\sqrt2\\,\\Phi^{{-1}}(0.75)={KAPPA:.4f},\\quad c={c:.5f}.
\\]

- 导数

\\[
\\frac{{dT}}{{d\\sigma_L^2}}=
\\frac{{\\kappa^2 c}}{{2\\sqrt{{A_0^2+\\kappa^2 c\\sigma_L^2}}}}>0
\\]

对所有 \\(\\sigma_L^2\\ge 0\\) 严格为正，因此阈值随局部方差单调上升。Monte Carlo 标定的离散阈值同样单调：Spearman \\(\\rho={rho:.3f}\\)；在“排序随机”零假设下，14 个点完全同序的精确双侧排列概率为 \\(2/14!\\approx{p_exact:.2e}\\)，全部相邻增量为正：`{monotonic}`。
- 令 \\(T(\\sigma_c^2)=A\\)，得到临界方差

\\[
\\boxed{{\\sigma_c^2=\\frac{{A^2-A_0^2}}{{\\kappa^2 c}}
={vc:.1f}\\ \\mathrm{{GL^2}}}}
\\]

bootstrap 95% 区间为 \\([{ci[0]:.1f},\\ {ci[1]:.1f}]\\ \\mathrm{{GL^2}}\\)。这里 \\(\\mathrm{{GL^2}}\\) 就是灰度平方。
- 三类区域实测方差跨度为 \\({rows[0]['v_meas']:.2f}\\) 到 \\({rows[-1]['v_meas']:.2f}\\ \\mathrm{{GL^2}}\\)，约 \\({rows[-1]['v_meas']/rows[0]['v_meas']:.0f}\\) 倍，足以把“可见—临界—掩蔽”三种状态拉开。

## 为什么同一量化步长观感不同

量化步长只决定可能引入的缝高 \\(A\\)；可见与否取决于 \\(A/T(\\sigma_L^2)\\)。平坦区 \\(\\sigma_L^2\\ll\\sigma_c^2\\)，阈值接近 \\(A_0\\)，所以 \\(A\\) 远高于阈值；纹理区 \\(\\sigma_L^2\\gg\\sigma_c^2\\)，缝跳变被同尺度的灰度起伏淹没。于是相同的 \\(q\\) 和相同的 \\(A\\)，在三类区域给出不同的归一化可见度。

## 三类区域汇总

{table_md}

判据：\\(V=\\hat A/T(\\hat\\sigma_L^2)\\)；\\(V>1\\) 超过 75% 可见阈值，\\(V<1\\) 被掩蔽。方差差为负表示位于临界方差以下，为正表示纹理方差已超过临界值。

## 主观对照图

- `subjective_flat.png` — 平坦区对照图
- `subjective_medium.png` — 中度纹理区对照图
- `subjective_strong.png` — 强纹理区对照图
- `masking_curve.png` — 阈值—方差曲线与临界方差
- `psychometric_curves.png` — 三类区域的心理测量曲线

每张区域对照图均包含无块缝图、同缝图、缝周放大、按 ±8 GL 色标显示的差值图、横向灰度剖面和本表关键数值。

## 复现与口径

```bash
python3 analysis/block_seam_masking.py --n-cal 4000
```

局部方差在缝两侧两个 32×32 窗内合并去均值后计算，排除中央 ±2 列，避免缝本身抬高方差。缝响应是一个 8 像素块段上的平均跨边跳变；\\(c\\) 由每个方差点 {n_cal} 个 Monte Carlo 样本标定。该实验是受控合成纹理上的计算观察者证明；主观 PNG 用于人眼复核，不把它冒称为真人实验数据。若把平坦区锚点 \\(A_0\\) 在 1–3 GL 间调整，临界方差见 `metrics.json` 的 sensitivity 字段，结论方向不变。
"""
    path = OUT / "report.md"
    path.write_text(md, encoding="utf-8")
    return path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--n-cal", type=int, default=4000)
    args = parser.parse_args()
    OUT.mkdir(parents=True, exist_ok=True)

    cal, samples, c, ci = calibrate(args.n_cal)
    vc = critical_variance(c)

    t_emp = cal["threshold_amp"].to_numpy()
    rho, _ = spearmanr(V_GRID, t_emp)
    p_exact = 2.0 / math.factorial(len(V_GRID))
    monotonic = bool(np.all(np.diff(t_emp) > 0.0))
    cal.to_csv(OUT / "calibration.csv", index=False, encoding="utf-8-sig")

    mc_rng = np.random.default_rng(SEED + 202)
    rows: list[dict] = []
    for i, spec in enumerate(CLASS_DEFS):
        clean, v_meas, natural_jump = choose_exemplar(spec["target_var"], i, c)
        seamed = add_seam(clean, SEAM_AMP)
        a_seamed = seam_response(seamed)
        a_meas = abs(a_seamed - natural_jump)
        a_threshold = af_threshold(v_meas, c)
        visibility = a_meas / a_threshold
        s_tot2 = SIGMA_D2 + c * v_meas
        dprime = a_meas / np.sqrt(s_tot2)

        # Calibration samples at the exact target variance supply the null D distribution.
        d_clean = samples[float(spec["target_var"])]
        p_curve, p8 = monte_carlo_detection(d_clean, float(spec["target_var"]), mc_rng)

        row = {
            **spec,
            "clean": clean,
            "seamed": seamed,
            "v_meas": float(v_meas),
            "vc": vc,
            "a_meas": float(a_meas),
            "a_raw_seamed": float(abs(a_seamed)),
            "natural_jump": float(natural_jump),
            "a_threshold": float(a_threshold),
            "visibility": float(visibility),
            "dprime": float(dprime),
            "p_mc_curve": p_curve,
            "p_mc8": float(p8),
            "c": c,
        }
        row["subjective_path"] = str(plot_subjective(row))
        rows.append(row)

    curve_path = plot_masking_curve(cal, c, vc, ci, rows)
    psycho_path = plot_psychometric(rows)
    report_path = write_markdown_report(cal, c, ci, vc, rows, float(rho), p_exact, monotonic, args.n_cal)

    print(f"c={c:.6f}")
    print(f"critical_variance={vc:.3f} GL^2  CI=[{ci[0]:.1f},{ci[1]:.1f}]")
    print(f"monotonic={monotonic} spearman={rho:.4f} exact_p={p_exact:.3e}")
    for r in rows:
        print(
            f"{r['key']:6s} v={r['v_meas']:8.2f} dv={r['v_meas']-vc:+9.2f} "
            f"A={r['a_meas']:.2f} T={r['a_threshold']:.2f} V={r['visibility']:.3f} "
            f"P2AFC={r['p_mc8']:.3f} {verdict_for(r['visibility'])[0]}"
        )
    print(f"wrote {report_path}")
    print(f"wrote {curve_path}")
    print(f"wrote {psycho_path}")


if __name__ == "__main__":
    main()
