#!/usr/bin/env python3
"""抖动强度—误差方差—RLE 体积的确定性实验。

运行：python3 analysis/dither_variance_study.py
输出：artifacts/dither-study/ 下的 CSV/JSON、四档误差图、总览图和 PGM 帧。

模型约定见 artifacts/dither-study/REPORT.md。脚本只依赖 numpy/matplotlib；
RLE 字节公式与 internal/codec/rle.go 一致，并用 ./build/gtk 做外部核对。
"""

from __future__ import annotations

import csv
import json
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager
from matplotlib.colors import LinearSegmentedColormap
import numpy as np

FONT_PATH = Path("/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf")
if FONT_PATH.exists():
    font_manager.fontManager.addfont(str(FONT_PATH))

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "artifacts" / "dither-study"
FRAMES = OUT / "frames"
OUT.mkdir(parents=True, exist_ok=True)
FRAMES.mkdir(parents=True, exist_ok=True)

# 与实验报告一致的调色板（蓝—中性灰—红，已经过 CVD 校验）。
plt.rcParams["font.family"] = "Noto Sans CJK SC"
plt.rcParams["axes.unicode_minus"] = False
INK = "#0b0b0b"
MUTED = "#52514e"
GRID = "#e1e0d9"
BLUE = "#2a78d6"
ORANGE = "#eb6834"
GREEN = "#008300"
DIVERGING = LinearSegmentedColormap.from_list(
    "blue_neutral_red", ["#2a78d6", "#f0efec", "#e34948"]
)

W = H = 256
N = W * H
L = 8
Q = 255.0 / (L - 1)
LEVELS = np.rint(np.linspace(0, 255, L))
BOUNDARIES = (LEVELS[:-1] + LEVELS[1:]) / 2.0
LAMBDA_BAND = 4.0
SEED = 20261126
ENSEMBLE_SEEDS = 32

TIERS = [
    (1, "关闭", 0.0),
    (2, "弱抖动", 1.0 / 3.0),
    (3, "中等偏强", 2.0 / 3.0),
    (4, "满档", 1.0),
]


def make_image() -> np.ndarray:
    """生成平滑梯度叠加缓变结构：可压缩，但误差图不是单一台阶。"""
    yy, xx = np.mgrid[0:H, 0:W]
    u = xx / (W - 1)
    v = yy / (H - 1)
    value = (
        255.0 * u
        + 10.0 * np.sin(2.0 * np.pi * v)
        + 8.0 * np.cos(2.0 * np.pi * (u + 0.7 * v))
        + 3.0 * np.sin(24.0 * u) * np.cos(17.0 * v)
    )
    return np.clip(np.rint(value), 0, 255).astype(np.uint8)


def quantize(x: np.ndarray) -> np.ndarray:
    idx = np.searchsorted(BOUNDARIES, np.asarray(x, dtype=float), side="right")
    return LEVELS[np.clip(idx, 0, L - 1)]


def dithered_quantize(img: np.ndarray, strength: float, seed: int) -> np.ndarray:
    rng = np.random.default_rng(seed)
    # U[-a,a]，其中 a=sQ/2；故 Var(D)=a^2/3=s^2 Q^2/12。
    noise = (rng.random(img.shape) - 0.5) * strength * Q
    return quantize(img.astype(float) + noise).astype(np.uint8)


def rle_byte_count(pix: np.ndarray) -> int:
    """精确复刻 internal/codec/rle.go：8 字节头 + ceil(run/255) 条 2 字节记录。"""
    flat = np.asarray(pix, dtype=np.uint8).ravel()
    if flat.size == 0:
        return 8
    change = np.flatnonzero(np.diff(flat.astype(np.int16)) != 0) + 1
    boundaries = np.r_[0, change, flat.size]
    runs = np.diff(boundaries)
    records = int(np.ceil(runs / 255.0).sum())
    return 8 + 2 * records


def write_pgm(path: Path, pix: np.ndarray) -> None:
    header = f"P5\n{W} {H}\n255\n".encode("ascii")
    path.write_bytes(header + np.asarray(pix, dtype=np.uint8).tobytes())


def box3_lowpass(e: np.ndarray) -> np.ndarray:
    padded = np.pad(e, 1, mode="edge")
    total = np.zeros_like(e, dtype=float)
    for dy in range(3):
        for dx in range(3):
            total += padded[dy : dy + H, dx : dx + W]
    return total / 9.0


def error_metrics(img: np.ndarray, out: np.ndarray) -> dict[str, float]:
    e = out.astype(float) - img.astype(float)
    low = box3_lowpass(e)
    high = e - low
    # 去掉 3 像素边界，避免图框和 edge padding 进入统计。
    sl = (slice(3, -3), slice(3, -3))
    raw_var = float(np.mean(e[sl] ** 2))
    band_var = float(np.mean(low[sl] ** 2))
    grain_var = float(np.mean(high[sl] ** 2))
    cross = float(np.mean(low[sl] * high[sl]))
    # Q=sqrt(lambda)*low, D=high；这是报告使用的感知加权总误差。
    total_var = LAMBDA_BAND * band_var + grain_var
    total_with_cross = total_var + 2.0 * np.sqrt(LAMBDA_BAND) * cross
    return {
        "raw_var": raw_var,
        "raw_rms": float(np.sqrt(raw_var)),
        "band_var": band_var,
        "band_rms": float(np.sqrt(band_var)),
        "grain_var": grain_var,
        "grain_rms": float(np.sqrt(grain_var)),
        "cross": cross,
        "total_var": float(total_var),
        "total_rms": float(np.sqrt(total_var)),
        "total_with_cross_rms": float(np.sqrt(max(total_with_cross, 0.0))),
    }


def save_error_map(
    path: Path, img: np.ndarray, out: np.ndarray, tier: tuple[int, str, float], m: dict
) -> None:
    idx, label, s = tier
    e = out.astype(float) - img.astype(float)
    fig, ax = plt.subplots(figsize=(8.2, 6.4), constrained_layout=True)
    im = ax.imshow(e, cmap=DIVERGING, vmin=-36, vmax=36, interpolation="nearest")
    ax.set_title(
        f"第 {idx} 档 · {label} · s={s:.3g}\n"
        f"加权总误差 RMS={m['total_rms']:.2f}；裸像素 RMSE={m['raw_rms']:.2f}",
        color=INK,
        fontsize=13,
        pad=10,
    )
    ax.set_xlabel("x / px", color=MUTED)
    ax.set_ylabel("y / px", color=MUTED)
    ax.tick_params(colors=MUTED)
    for spine in ax.spines.values():
        spine.set_color(GRID)
    cbar = fig.colorbar(im, ax=ax, fraction=0.046, pad=0.03)
    cbar.set_label("量化输出 − 原图 / 灰度级", color=MUTED)
    cbar.ax.tick_params(colors=MUTED)
    cbar.outline.set_edgecolor(GRID)
    fig.savefig(path, dpi=180, facecolor="white")
    plt.close(fig)


def save_summary(path: Path, rows: list[dict], s_star: float) -> None:
    s = np.array([r["strength"] for r in rows], dtype=float)
    rms = np.array([r["total_rms"] for r in rows], dtype=float)
    sizes = np.array([r["rle_bytes"] for r in rows], dtype=float)
    best_i = int(np.argmin(rms))
    size_i = int(np.argmin(sizes))

    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(9.2, 7.6), constrained_layout=True)

    ax1.plot(s, rms, color=BLUE, linewidth=2.2, marker="o", markersize=8)
    ax1.scatter(
        [s[best_i]], [rms[best_i]], s=190, marker="*", color=GREEN,
        edgecolor="white", linewidth=1.2, zorder=5, label="四档中的最优档",
    )
    ax1.axvline(s_star, color=MUTED, linestyle="--", linewidth=1.4)
    ax1.annotate(
        f"解析极小 s*={s_star:.3f}",
        xy=(s_star, rms[best_i]),
        xytext=(8, 18),
        textcoords="offset points",
        color=INK,
        fontsize=10,
        arrowprops=dict(arrowstyle="->", color=MUTED, lw=1),
    )
    for x, y in zip(s, rms):
        ax1.annotate(f"{y:.2f}", (x, y), textcoords="offset points", xytext=(0, 9),
                     ha="center", fontsize=9, color=INK)
    ax1.set_title("总误差（方差平方根）先降后升", fontsize=14, color=INK, pad=8)
    ax1.set_ylabel("加权总误差 RMS / 灰度级")
    ax1.set_xticks(s)
    ax1.grid(True, color=GRID, linewidth=0.8)
    ax1.legend(frameon=False, loc="upper center")

    ax2.plot(s, sizes, color=ORANGE, linewidth=2.2, marker="s", markersize=8)
    ax2.scatter([s[size_i]], [sizes[size_i]], s=150, facecolor="white",
                edgecolor=ORANGE, linewidth=2.2, zorder=5, label="体积极小点")
    for x, y in zip(s, sizes):
        ax2.annotate(f"{int(y):,} B", (x, y), textcoords="offset points",
                     xytext=(0, 10), ha="center", fontsize=9, color=INK)
    ax2.set_title("RLE 体积单调上升，极小点在 s=0", fontsize=14, color=INK, pad=8)
    ax2.set_xlabel("抖动强度 s（a=sQ/2）")
    ax2.set_ylabel("RLE 编码体积 / bytes")
    ax2.set_xticks(s)
    ax2.grid(True, color=GRID, linewidth=0.8)
    ax2.legend(frameon=False, loc="upper left")

    for ax in (ax1, ax2):
        ax.set_facecolor("#fcfcfb")
        for spine in ("top", "right"):
            ax.spines[spine].set_visible(False)
        for spine in ("left", "bottom"):
            ax.spines[spine].set_color("#c3c2b7")
        ax.tick_params(colors=MUTED)
    fig.savefig(path, dpi=180, facecolor="white")
    plt.close(fig)


def main() -> None:
    img = make_image()
    write_pgm(FRAMES / "source.pgm", img)

    rows: list[dict] = []
    ensemble: dict[str, dict] = {}

    for idx, label, s in TIERS:
        out = dithered_quantize(img, s, SEED)
        pgm_path = FRAMES / f"tier{idx}_s{s:.4f}.pgm"
        write_pgm(pgm_path, out)
        m = error_metrics(img, out)
        size = rle_byte_count(out)
        noise_var = (s * Q / 2.0) ** 2 / 3.0
        row = {
            "tier": idx,
            "label": label,
            "strength": s,
            "dither_half_width": s * Q / 2.0,
            "noise_var": noise_var,
            "noise_rms": float(np.sqrt(noise_var)),
            "cheb95_total_bound": float(np.sqrt(20.0 * m["total_var"])),
            "cheb95_noise_bound": float(np.sqrt(20.0 * noise_var)),
            "rle_bytes": size,
            "pgm": str(pgm_path.relative_to(ROOT)),
            **m,
        }
        rows.append(row)

        map_path = OUT / f"tier{idx}_error_s{s:.4f}.png"
        save_error_map(map_path, img, out, (idx, label, s), m)
        row["error_map"] = str(map_path.relative_to(ROOT))

        # 多种子平均只用于确认固定种子没有偶然偏置。
        total_vars, byte_counts = [], []
        for seed_offset in range(ENSEMBLE_SEEDS):
            ens_out = dithered_quantize(img, s, SEED + 7919 * seed_offset)
            ens_m = error_metrics(img, ens_out)
            total_vars.append(ens_m["total_var"])
            byte_counts.append(rle_byte_count(ens_out))
        ensemble[f"tier{idx}"] = {
            "seeds": ENSEMBLE_SEEDS,
            "total_rms_mean": float(np.sqrt(np.mean(total_vars))),
            "total_rms_sd_across_seeds": float(np.std(np.sqrt(total_vars))),
            "rle_bytes_mean": float(np.mean(byte_counts)),
            "rle_bytes_sd": float(np.std(byte_counts)),
        }

    # 用离散最优档及其左右两档做等距三点抛物线插值。
    best_i = min(range(len(rows)), key=lambda i: rows[i]["total_rms"])
    lo, hi = max(0, best_i - 1), min(len(rows) - 1, best_i + 1)
    if hi - lo == 2:
        s0 = rows[best_i]["strength"]
        h = rows[best_i]["strength"] - rows[lo]["strength"]
        v_minus = rows[lo]["total_var"]
        v0 = rows[best_i]["total_var"]
        v_plus = rows[hi]["total_var"]
        # V(t)=A+Bt+Ct^2, t=(s-s0)/h ∈ {-1,0,1}
        a = v0
        b = (v_plus - v_minus) / 2.0
        c = (v_plus - 2.0 * v0 + v_minus) / 2.0
        t_star = -b / (2.0 * c)
        s_star = s0 + h * t_star
        v_star = a - b * b / (4.0 * c)
        optimum = {
            "method": "equal-spacing three-point quadratic interpolation",
            "indices": [lo + 1, best_i + 1, hi + 1],
            "h_strength": h,
            "A_t": a,
            "B_t": b,
            "C_t": c,
            "t_star": t_star,
            "s_star": s_star,
            "variance_star": v_star,
            "rms_star": float(np.sqrt(max(v_star, 0.0))),
            "strict_minimum": bool(c > 0),
        }
    else:
        optimum = {"method": "discrete only", "s_star": rows[best_i]["strength"]}
        s_star = rows[best_i]["strength"]

    save_summary(OUT / "tradeoff.png", rows, float(s_star))

    csv_path = OUT / "metrics.csv"
    fields = [
        "tier", "label", "strength", "total_rms", "total_var", "raw_rms",
        "band_rms", "grain_rms", "cross", "noise_rms", "cheb95_total_bound",
        "cheb95_noise_bound", "rle_bytes", "dither_half_width", "error_map", "pgm",
    ]
    with csv_path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fields)
        writer.writeheader()
        for r in rows:
            writer.writerow({k: r[k] for k in fields})

    result = {
        "config": {
            "width": W,
            "height": H,
            "pixels": N,
            "levels": L,
            "quant_step_q": Q,
            "lambda_band": LAMBDA_BAND,
            "seed": SEED,
            "ensemble_seeds": ENSEMBLE_SEEDS,
            "dither_distribution": "U[-s*q/2, s*q/2]",
            "weighted_error_definition": "V=4*Var(low)+Var(high)",
        },
        "optimum": optimum,
        "size_min_tier": min(rows, key=lambda r: r["rle_bytes"])["tier"],
        "error_min_tier": best_i + 1,
        "coincides_with_size_min": rows[best_i]["rle_bytes"] == min(r["rle_bytes"] for r in rows),
        "rows": rows,
        "ensemble": ensemble,
    }
    (OUT / "results.json").write_text(
        json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8"
    )

    print(f"wrote {csv_path.relative_to(ROOT)}")
    print(f"error optimum tier={best_i + 1}, s*={optimum.get('s_star'):.6f}, "
          f"R*={optimum.get('rms_star'):.4f}")
    for r in rows:
        mark = " *" if r["tier"] == best_i + 1 else ""
        print(
            f"tier{r['tier']} s={r['strength']:.4f} "
            f"R={r['total_rms']:.3f} V={r['total_var']:.2f} "
            f"noiseRMS={r['noise_rms']:.3f} B={r['rle_bytes']}{mark}"
        )


if __name__ == "__main__":
    main()
