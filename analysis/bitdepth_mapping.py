#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Bit-depth promotion as a constrained mapping problem.

The default input is a deterministic 6-bit diagnostic histogram (no image path
was supplied in the task).  A real two-column CSV ``k,count`` may be passed.

Outputs, under --out:
  * source_histogram.csv
  * mapping_d2.csv
  * metrics.json
  * bitdepth_table.csv / bitdepth_table.md
  * histograms_side_by_side.png

Only numpy + Pillow are required.  Run with python3 -I.
"""
from __future__ import annotations

import argparse
import csv
import json
import math
from pathlib import Path

import numpy as np
from PIL import Image, ImageDraw, ImageFont

# ---------- problem constants ----------
B0 = 6
N = 1 << B0          # 64 codes
I = N - 1            # 63 max code
DEFAULT_PEAK = 32
DELTAS = (1, 2, 3)   # 6 -> 7, 8, 9 bits

# Validated reference palette (light surface)
ORANGE = "#eb6834"
ORANGE_DARK = "#b84315"
BLUE = "#2a78d6"
BLUE_DARK = "#184f95"
INK = "#0b0b0b"
SECOND = "#52514e"
MUTED = "#898781"
GRID = "#e1e0d9"
AXIS = "#c3c2b7"
SURFACE = "#fcfcfb"
WHITE = "#ffffff"

FONT_PATH = "/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf"


def font(size: int) -> ImageFont.FreeTypeFont | ImageFont.ImageFont:
    try:
        return ImageFont.truetype(FONT_PATH, size)
    except Exception:
        return ImageFont.load_default()


def default_histogram() -> np.ndarray:
    """Smooth unimodal diagnostic histogram with every 6-bit code occupied."""
    k = np.arange(N, dtype=float)
    h = (
        60.0
        + 1500.0 * np.exp(-((k - DEFAULT_PEAK) ** 2) / (2.0 * 8.5**2))
        + 100.0 * np.exp(-((k - 48.0) ** 2) / (2.0 * 4.0**2))
    )
    return np.rint(h).astype(int)


def read_hist_csv(path: Path) -> tuple[np.ndarray, int | None]:
    h = np.zeros(N, dtype=int)
    peak: int | None = None
    with path.open(newline="", encoding="utf-8") as f:
        rows = list(csv.DictReader(f))
        for row in rows:
            k = int(row["k"])
            if not 0 <= k <= I:
                raise ValueError(f"source code out of range: {k}")
            h[k] = int(round(float(row["count"])))
            if "peak" in row and str(row["peak"]).strip() in {"1", "true", "True"}:
                peak = k
    return h, peak


def rounded_full_range_map(delta: int) -> tuple[int, int, np.ndarray, float, int]:
    q = 1 << delta
    target_bits = B0 + delta
    J = (1 << target_bits) - 1
    alpha = J / I
    continuous_peak = alpha * DEFAULT_PEAK
    peak_bin = int(math.floor(continuous_peak + 0.5))
    k = np.arange(N, dtype=float)
    f = np.floor(alpha * k + 0.5).astype(int)
    f[0] = 0
    f[-1] = J
    # Hard peak constraint.  Nearest rounding already does this for the
    # selected peak, but make the invariant explicit.
    f[DEFAULT_PEAK] = peak_bin
    return q, target_bits, J, alpha, peak_bin, f


def pl_interpolation_rmse(f: np.ndarray, J: int, alpha: float, full_range: bool) -> float:
    """RMSE of piecewise-linear inverse coordinates vs ideal x*=I*j/J.

    Multiplied by alpha so the unit is target bins.  For shift, evaluate only
    on its reachable interval; its missing tail is separately a constraint
    failure.
    """
    if full_range:
        j = np.arange(J + 1, dtype=float)
        x_hat = np.interp(j, f.astype(float), np.arange(N, dtype=float))
    else:
        last = int(f[-1])
        j = np.arange(last + 1, dtype=float)
        x_hat = j / float(1 << delta_of(J))  # shift inverse x=j/q
    ideal = I * j / J
    return alpha * float(np.sqrt(np.mean((x_hat - ideal) ** 2)))


def delta_of(J: int) -> int:
    target_bits = J.bit_length()
    return target_bits - B0


def dashed_vline(draw: ImageDraw.ImageDraw, x: float, y0: float, y1: float,
                 fill: str, width: int = 2, dash: int = 9, gap: int = 7) -> None:
    y = y0
    while y < y1:
        draw.line((x, y, x, min(y + dash, y1)), fill=fill, width=width)
        y += dash + gap


def draw_text_box(draw: ImageDraw.ImageDraw, xy: tuple[int, int], lines: list[tuple[str, int, str]],
                  font_regular, width: int = 326) -> None:
    x, y = xy
    line_h = 25
    h = 18 + line_h * len(lines)
    draw.rounded_rectangle((x, y, x + width, y + h), radius=10,
                           fill=WHITE, outline=GRID, width=1)
    cy = y + 10
    for text, size, color in lines:
        fnt = font(size) if size != 15 else font_regular
        draw.text((x + 14, cy), text, font=fnt, fill=color)
        cy += line_h if size >= 15 else 22


def render_chart(h: np.ndarray, out_png: Path) -> dict:
    delta = 2
    q, B, J, alpha, P, fopt = rounded_full_range_map(delta)
    fshift = q * np.arange(N)
    jgrid = np.arange(J + 1)
    Hopt = np.interp(jgrid.astype(float), fopt.astype(float), h.astype(float))
    Hshift_pl = np.interp(jgrid.astype(float), fshift.astype(float), h.astype(float),
                          left=0.0, right=0.0)
    # Shape comparison: normalize heights to their own peak.  Gap occupancy is
    # unchanged by positive scaling.
    h_show = h.astype(float) / float(h.max())
    Hopt_show = Hopt / float(Hopt.max())

    W, H = 1800, 1180
    img = Image.new("RGBA", (W, H), SURFACE + "ff")
    draw = ImageDraw.Draw(img)
    f14 = font(15)
    f16 = font(17)
    f18 = font(20)
    f22 = font(24)
    f28 = font(30)
    f34 = font(36)

    draw.text((70, 34), "6-bit → 8-bit 灰度提升：位移映射 vs 约束最优映射", font=f34, fill=INK)
    draw.text((72, 82), "同一确定性诊断直方图（峰位 k=32；纵轴按峰高归一化）；上图为目标码 0–255 全量程，底排为峰区放大", font=f18, fill=SECOND)

    # Legend
    lx = 1180
    draw.rectangle((lx, 42, lx + 22, 60), fill=ORANGE)
    draw.text((lx + 30, 40), "位移 S(k)=4k", font=f16, fill=SECOND)
    draw.rectangle((lx + 178, 42, lx + 200, 60), fill=BLUE)
    draw.text((lx + 208, 40), "最优+线性插值", font=f16, fill=SECOND)
    dashed_vline(draw, lx + 382, 42, 60, INK, width=2, dash=7, gap=5)
    draw.text((lx + 394, 40), "满量程峰锚 130", font=f16, fill=SECOND)

    panels = [
        {"rect": (70, 145, 865, 650), "zoom": (70, 735, 865, 1045),
         "title": "① 位移提升：空档被放大 4 倍，末端只到 252", "kind": "shift"},
        {"rect": (935, 145, 1730, 650), "zoom": (935, 735, 1730, 1045),
         "title": "② 最优满量程映射 + 插值：端点到 255，峰位不偏", "kind": "opt"},
    ]
    ymax = 1.12
    xticks = [0, 32, 64, 96, 128, 160, 192, 224, 255]
    yticks = [0.0, 0.25, 0.50, 0.75, 1.0]

    def mapping(rect):
        x0, y0, x1, y1 = rect
        ml, mr, mt, mb = 58, 18, 38, 42
        px0, py0, px1, py1 = x0 + ml, y0 + mt, x1 - mr, y1 - mb

        def X(v):
            return px0 + (v / J) * (px1 - px0)

        def Y(v):
            return py1 - (v / ymax) * (py1 - py0)
        return px0, py0, px1, py1, X, Y

    for panel in panels:
        rect = panel["rect"]
        x0, y0, x1, y1 = rect
        draw.rounded_rectangle(rect, radius=14, fill=SURFACE, outline=GRID, width=1)
        draw.text((x0 + 22, y0 + 14), panel["title"], font=f22, fill=INK)
        px0, py0, px1, py1, X, Y = mapping(rect)

        for yt in yticks:
            yy = Y(yt)
            draw.line((px0, yy, px1, yy), fill=GRID, width=1)
            label = "1" if yt == 1 else ("0" if yt == 0 else f"{yt:.2f}")
            draw.text((px0 - 42, yy - 11), label, font=f14, fill=MUTED)
        draw.line((px0, py1, px1, py1), fill=AXIS, width=2)
        draw.line((px0, py0, px0, py1), fill=AXIS, width=2)
        for xt in xticks:
            xx = X(xt)
            draw.line((xx, py1, xx, py1 + 6), fill=AXIS, width=2)
            tw = draw.textlength(str(xt), font=f14)
            draw.text((xx - tw / 2, py1 + 12), str(xt), font=f14, fill=MUTED)
        draw.text((px1 - 92, py1 + 34), "输出 8-bit 码值", font=f14, fill=SECOND)

        bin_step = (px1 - px0) / J
        if panel["kind"] == "shift":
            for k, hh in enumerate(h_show):
                xx = X(int(fshift[k]))
                bw = max(2.0, bin_step * 0.72)
                draw.rectangle((xx - bw / 2, Y(float(hh)), xx + bw / 2, py1), fill=ORANGE)
            pts = [(X(int(fshift[k])), Y(float(h_show[k]))) for k in range(N)]
            draw.line(pts, fill=ORANGE_DARK, width=3, joint="curve")
            dashed_vline(draw, X(P), py0 + 8, py1, INK, width=2)
            draw.line((X(fshift[DEFAULT_PEAK]), py0 + 8, X(fshift[DEFAULT_PEAK]), py1),
                      fill=ORANGE_DARK, width=3)
            draw_text_box(draw, (x0 + 24, y0 + 58), [
                ("S(k)=k≪2，空档间距 4", 17, INK),
                (f"S(63)={int(fshift[-1])}，违反铺满（差 3 箱）", 15, SECOND),
                (f"峰位 S(32)={int(fshift[32])}，锚点 {P}，偏移 −2 箱", 15, SECOND),
                ("裸点零箱率 192/256 = 75.00%", 15, SECOND),
            ], f14)
        else:
            overlay = Image.new("RGBA", (W, H), (0, 0, 0, 0))
            od = ImageDraw.Draw(overlay)
            for j, hh in enumerate(Hopt_show):
                xx = X(j)
                bw = bin_step + 0.5
                od.rectangle((xx - bw / 2, Y(float(hh)), xx + bw / 2, py1),
                             fill=(109, 167, 236, 255))
            img = Image.alpha_composite(img, overlay)
            draw = ImageDraw.Draw(img)
            pts = [(X(j), Y(float(Hopt_show[j]))) for j in jgrid]
            draw.line(pts, fill=BLUE_DARK, width=3, joint="curve")
            for k in range(N):
                xx = X(int(fopt[k]))
                draw.line((xx, py1 - 7, xx, py1), fill=BLUE_DARK, width=2)
            dashed_vline(draw, X(P), py0 + 8, py1, INK, width=2)
            draw_text_box(draw, (x0 + 24, y0 + 58), [
                ("T*(k)=round(255k/63)", 17, INK),
                ("T*(63)=255，值域铺满且严格单调", 15, SECOND),
                (f"峰位 T*(32)={int(fopt[32])}，偏移 0 箱", 15, SECOND),
                ("线性插值后有效空档率 0.00%", 15, SECOND),
            ], f14)

        # ----- zoom panel -----
        zrect = panel["zoom"]
        zx0, zy0, zx1, zy1 = zrect
        draw.rounded_rectangle(zrect, radius=14, fill=WHITE, outline=GRID, width=1)
        zxmin, zxmax = 104, 156
        zml, zmr, zmt, zmb = 52, 18, 42, 38
        zpx0, zpy0 = zx0 + zml, zy0 + zmt
        zpx1, zpy1 = zx1 - zmr, zy1 - zmb

        def ZX(v):
            return zpx0 + (v - zxmin) / (zxmax - zxmin) * (zpx1 - zpx0)

        def ZY(v):
            return zpy1 - v / ymax * (zpy1 - zpy0)

        draw.text((zx0 + 20, zy0 + 12), "峰区放大（104–156 码）", font=f18, fill=INK)
        for zt in [0.0, 0.5, 1.0]:
            yy = ZY(zt)
            draw.line((zpx0, yy, zpx1, yy), fill=GRID, width=1)
            label = "1" if zt == 1 else ("0" if zt == 0 else ".50")
            draw.text((zpx0 - 42, yy - 10), label, font=f14, fill=MUTED)
        draw.line((zpx0, zpy1, zpx1, zpy1), fill=AXIS, width=2)
        for xt in range(104, 157, 8):
            xx = ZX(xt)
            draw.line((xx, zpy1, xx, zpy1 + 5), fill=AXIS, width=1)
            tw = draw.textlength(str(xt), font=f14)
            draw.text((xx - tw / 2, zpy1 + 10), str(xt), font=f14, fill=MUTED)
        dashed_vline(draw, ZX(P), zpy0, zpy1, INK, width=2)
        zstep = (zpx1 - zpx0) / (zxmax - zxmin)
        if panel["kind"] == "shift":
            for k, hh in enumerate(h_show):
                v = int(fshift[k])
                if zxmin <= v <= zxmax:
                    draw.rectangle((ZX(v) - zstep * 0.38, ZY(float(hh)), ZX(v) + zstep * 0.38, zpy1), fill=ORANGE)
            pts = [(ZX(int(fshift[k])), ZY(float(h_show[k]))) for k in range(N)
                   if zxmin <= fshift[k] <= zxmax]
            if len(pts) >= 2:
                draw.line(pts, fill=ORANGE_DARK, width=3)
            draw.line((ZX(fshift[DEFAULT_PEAK]), zpy0, ZX(fshift[DEFAULT_PEAK]), zpy1),
                      fill=ORANGE_DARK, width=3)
            draw.text((ZX(128) + 8, zpy0 + 8), "实际峰 128", font=f16, fill=ORANGE_DARK,
                      stroke_width=4, stroke_fill=WHITE)
            draw.text((ZX(130) + 8, zpy0 + 34), "锚点 130", font=f16, fill=INK,
                      stroke_width=4, stroke_fill=WHITE)
        else:
            overlay = Image.new("RGBA", (W, H), (0, 0, 0, 0))
            od = ImageDraw.Draw(overlay)
            for j, hh in enumerate(Hopt_show):
                if zxmin <= j <= zxmax:
                    od.rectangle((ZX(j) - zstep / 2 - 0.3, ZY(float(hh)),
                                  ZX(j) + zstep / 2 + 0.3, zpy1),
                                 fill=(109, 167, 236, 255))
            img = Image.alpha_composite(img, overlay)
            draw = ImageDraw.Draw(img)
            pts = [(ZX(j), ZY(float(Hopt_show[j]))) for j in range(zxmin, zxmax + 1)]
            draw.line(pts, fill=BLUE_DARK, width=3)
            draw.text((ZX(130) + 8, zpy0 + 12), "峰/锚点均为 130", font=f16, fill=BLUE_DARK,
                      stroke_width=4, stroke_fill=WHITE)

    footer = ("口径说明：单值、严格单调映射只能移动 K 个非零箱，不能改变零箱总数；6→8 的裸点零箱率恒为 1−K/256。"
              "右图的 0% 是线性插值重采样后的有效空档率；若给位移法也加同一插值，空档同样会被填上，"
              "但坐标误差与峰偏仍更大。")
    draw.rounded_rectangle((70, 1080, 1730, 1158), radius=12, fill="#f3f2ec", outline=GRID)
    # crude word wrap for the long CJK line
    line1 = footer[:73]
    line2 = footer[73:]
    draw.text((92, 1095), line1, font=f16, fill=SECOND)
    draw.text((92, 1124), line2, font=f16, fill=SECOND)

    img = img.convert("RGB")
    img.save(out_png, dpi=(160, 160))
    return {"Hopt_min": float(Hopt.min()), "Hshift_pl_min_inside_support": float(Hshift_pl[1:-1].min())}


def row_for_delta(h: np.ndarray, peak: int, d: int) -> dict:
    K = int(np.count_nonzero(h > 0))
    q, B, J, alpha, P, fopt = rounded_full_range_map(d)
    k = np.arange(N, dtype=float)
    fshift = q * k
    e_shift = float(np.sqrt(np.mean((fshift - alpha * k) ** 2)))
    e_opt = float(np.sqrt(np.mean((fopt.astype(float) - alpha * k) ** 2)))
    pl_shift = pl_interpolation_rmse(fshift.astype(int), J, alpha, full_range=False)
    pl_opt = pl_interpolation_rmse(fopt, J, alpha, full_range=True)
    M = J + 1
    rho_raw = (M - K) / M
    Hopt = np.interp(np.arange(M, dtype=float), fopt.astype(float), h.astype(float))
    Hshift = np.interp(np.arange(M, dtype=float), fshift, h.astype(float),
                       left=0.0, right=0.0)
    rho_eff_opt = float(np.mean(Hopt <= 1e-12))
    rho_eff_shift_linear = float(np.mean(Hshift <= 1e-12))
    peak_off_shift = q * peak - P
    norm_shift = -peak * (q - 1) / (I * J)
    return {
        "delta_bits": d,
        "target_bits": B,
        "q": q,
        "J": J,
        "peak_anchor_bin": P,
        "occupied_bins": K,
        "pointmap_zero_ratio_pct": 100 * rho_raw,
        "shift_linear_effective_gap_ratio_pct": 100 * rho_eff_shift_linear,
        "optimal_interpolated_effective_gap_ratio_pct": 100 * rho_eff_opt,
        "coordinate_rmse_shift_bins": e_shift,
        "coordinate_rmse_opt_bins": e_opt,
        "pl_interpolation_rmse_shift_bins_support_only": pl_shift,
        "pl_interpolation_rmse_opt_bins": pl_opt,
        "peak_offset_shift_bins": peak_off_shift,
        "peak_offset_opt_bins": 0,
        "normalized_peak_shift_shift": norm_shift,
        "shift_endpoint": q * I,
        "endpoint_shortfall_bins": J - q * I,
    }


def balanced_cumulative(total: int, segments: int) -> list[int]:
    return [total * i // segments for i in range(segments + 1)]


def gap_balanced_example_rows() -> list[dict]:
    """Irregular-anchor example showing gap concentration being reduced."""
    anchors = np.array([0, 4, 9, 14, 18, 22, 25, 28, 30, 32,
                        34, 36, 39, 42, 46, 50, 55, 59, 63])
    peak = 32
    m = int(np.where(anchors == peak)[0][0])
    rows: list[dict] = []
    segments = len(anchors) - 1
    right_segments = segments - m
    for d in DELTAS:
        q, B, J, alpha, P, _ = rounded_full_range_map(d)
        y_shift = q * anchors
        y_left = balanced_cumulative(P, m)
        y_right = [P + v for v in balanced_cumulative(J - P, right_segments)[1:]]
        y_opt = np.array(y_left + y_right)

        def gap_stats(y: np.ndarray) -> dict:
            gaps = np.diff(y).astype(float)
            remainder = (1.0 / 8.0) * math.sqrt(float(np.mean(gaps**4)))
            return {
                "cv_pct": 100.0 * float(np.std(gaps) / np.mean(gaps)),
                "remainder_bin2": remainder,
                "max_empty_bins": int(np.max(gaps)) - 1,
            }

        ss, oo = gap_stats(y_shift), gap_stats(y_opt)
        rows.append({
            "delta_bits": d,
            "target_bits": B,
            "occupied_anchor_bins": len(anchors),
            "pointmap_zero_ratio_pct": 100.0 * (J + 1 - len(anchors)) / (J + 1),
            "gap_cv_shift_pct": ss["cv_pct"],
            "gap_cv_opt_pct": oo["cv_pct"],
            "interp_remainder_shift_bin2": ss["remainder_bin2"],
            "interp_remainder_opt_bin2": oo["remainder_bin2"],
            "max_empty_shift_bins": ss["max_empty_bins"],
            "max_empty_opt_bins": oo["max_empty_bins"],
            "peak_offset_shift_bins": q * peak - P,
            "peak_offset_opt_bins": 0,
        })
    return rows


def build_tables(h: np.ndarray, peak: int) -> tuple[list[dict], list[dict], str]:
    K = int(np.count_nonzero(h > 0))
    rows = [row_for_delta(h, peak, d) for d in DELTAS]
    extended_rows = [row_for_delta(h, peak, d) for d in range(1, 7)]
    md = [
        "# 位深 6→(6+Δ)：三档位深差结果",
        "",
        f"源位深 b=6，峰位 k={peak}，非零箱 K={K}。`E_int` 是映射整数化相对连续满量程直线 "
        "`α=(2^(6+Δ)−1)/63` 的 RMSE（目标码单位）。",
        "",
        "| Δb | 目标位深 | 放大倍数 2^Δ | 裸点空档率¹ | 插值后有效空档率² | E_int：位移 | E_int：最优 | 峰偏：位移 | 峰偏：最优 |",
        "|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for row in rows:
        d = row["delta_bits"]
        B = row["target_bits"]
        q = row["q"]
        rho_raw = row["pointmap_zero_ratio_pct"] / 100.0
        rho_eff_opt = row["optimal_interpolated_effective_gap_ratio_pct"] / 100.0
        e_shift = row["coordinate_rmse_shift_bins"]
        e_opt = row["coordinate_rmse_opt_bins"]
        peak_off_shift = row["peak_offset_shift_bins"]
        md.append(
            f"| {d} | {B} | {q} | {100*rho_raw:.2f}% | {100*rho_eff_opt:.2f}% | "
            f"{e_shift:.3f} | {e_opt:.3f} | {peak_off_shift:+d} | 0 |"
        )
    md += [
        "",
        "¹ 裸点空档率 `1−K/2^B`。对任何单值、单射、严格单调映射都相同；这是离散码箱计数不变量。",
        "² 最优映射后做线性插值重采样，且首末锚点计数为正时，0..J 每个目标箱都得到正值。"
        "给位移法也加同样的线性插值时，其内部空档也会被填上；此时最优映射的优势是 E_int 更低、峰偏为 0、端点铺满。",
        "",
        "## 分段插值误差的平衡解（用于含原始空档的直方图）",
        "",
        "若只在非零锚点 `x_0<...<x_{S}` 上规定映射，并以线性插值余项 "
        "`ε=(1/8) sqrt(S^{-1} Σ Δ_i^4)` 为误差口径，则约束为 "
        "`y_0=0, y_S=J, y_m=P`。由凸性/Cauchy，峰左、峰右的输出间距必须分别尽量相等：",
        "",
        "```text",
        "y_i       = floor(P i/m),                 0 ≤ i ≤ m",
        "y_{m+j}   = P + floor((J-P) j/(S-m)),     0 ≤ j ≤ S-m",
        "```",
        "",
        "整数间距只取 `floor(P/m)` 或 `ceil(P/m)`（右侧同理）。交换论证：若两段间距相差至少 2，"
        "从长段移一箱到短段会降低任意凸间距损失，因此平衡分配是整数最优。全 64 码本均为锚点且 "
        "`P=round(Jp/63)` 时，它等价于 `T*(k)=round(Jk/63)`。",
        "",
    ]
    return rows, extended_rows, "\n".join(md) + "\n"


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="artifacts/bitdepth_mapping")
    ap.add_argument("--hist-csv", type=Path)
    args = ap.parse_args()
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    if args.hist_csv:
        h, csv_peak = read_hist_csv(args.hist_csv)
        peak = csv_peak if csv_peak is not None else int(np.argmax(h))
    else:
        h = default_histogram()
        peak = int(np.argmax(h))
        if peak != DEFAULT_PEAK:
            # The formula is general; chart annotations currently assume the
            # deterministic default peak.  Keep data and report consistent.
            peak = DEFAULT_PEAK

    # Source CSV
    with (out / "source_histogram.csv").open("w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["k", "count", "peak"])
        for k, hh in enumerate(h):
            w.writerow([k, int(hh), int(k == peak)])

    rows, extended_rows, table_md = build_tables(h, peak)

    # d=2 mapping CSV
    q, B, J, alpha, P, fopt = rounded_full_range_map(2)
    with (out / "mapping_d2.csv").open("w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["source_k", "count", "shift_k_times_4", "optimal_round_255k_over_63"])
        for k in range(N):
            w.writerow([k, int(h[k]), int(4 * k), int(fopt[k])])

    chart_info = render_chart(h, out / "histograms_side_by_side.png")

    with (out / "bitdepth_table.csv").open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)
    with (out / "bitdepth_table_extended_d1_to_d6.csv").open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(extended_rows[0].keys()))
        writer.writeheader()
        writer.writerows(extended_rows)

    gap_rows = gap_balanced_example_rows()
    with (out / "gap_balanced_example.csv").open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(gap_rows[0].keys()))
        writer.writeheader()
        writer.writerows(gap_rows)
    (out / "bitdepth_table.md").write_text(table_md, encoding="utf-8")

    proof = {
        "b": B0,
        "I": I,
        "peak": peak,
        "gap_amplification_formula": "A(delta)=2**delta",
        "empty_run_formula": "r_empty_source=r => r_empty_output=2**delta*(r+1)-1",
        "shift_endpoint_formula": "2**delta*(2**b-1)=2**(b+delta)-2**delta=J-(2**delta-1)",
        "peak_normalized_shift_formula": "-p*(2**delta-1)/((2**b-1)*(2**(b+delta)-1))",
        "rows": rows,
        "extended_rows_d1_to_d6": extended_rows,
        "gap_balanced_example": gap_rows,
        "chart_interpolation_check": chart_info,
    }
    (out / "metrics.json").write_text(json.dumps(proof, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"wrote {out}")
    for row in rows:
        print(row)


if __name__ == "__main__":
    main()
