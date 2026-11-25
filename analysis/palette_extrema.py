#!/usr/bin/env python3 -I
"""Finite-candidate palette extremum validation.

The script builds three deterministic Lab calibration charts, compresses them
with a coarse palette and a boundary-refined palette, checks the predicted
extremum coordinates against the measured argmax, and writes discrete heatmaps.

The extremum theorem used here is exact for the setting implemented below:
Euclidean Delta E 76 in CIE Lab, straight palette/color trajectories, and
nearest-center quantization.  It is not a theorem for CIEDE2000 or for an
arbitrary non-collinear Voronoi palette; see PALETTE_REPORT.md.
"""

from __future__ import annotations

import csv
import json
import math
from pathlib import Path

import numpy as np
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "artifacts" / "palette_extrema"

W, H = 208, 144
SCALE = 4
PERP_HALF_WIDTH = 1.15

# Eight fixed Delta-E bins, shared by every before/after heatmap.
EDGES = np.array([0.0, 1.0, 2.0, 3.0, 5.0, 8.0, 12.0, 18.0, math.inf])
BIN_LABELS = [
    "0-1",
    "1-2",
    "2-3",
    "3-5",
    "5-8",
    "8-12",
    "12-18",
    ">=18",
]
HEAT_COLORS = np.array(
    [
        [255, 245, 240],
        [254, 224, 210],
        [252, 187, 161],
        [252, 146, 114],
        [251, 106, 74],
        [239, 59, 44],
        [203, 24, 29],
        [153, 0, 13],
    ],
    dtype=np.uint8,
)

# Lab coordinates of the sRGB gamut-boundary primaries used as ray endpoints.
CASES = [
    {
        "name": "case1_warm_red",
        "title": "Chart 1: neutral to red gamut boundary",
        "p0": (20.0, 118.0),
        "p1": (188.0, 24.0),
        "base": np.array([69.0, 5.0, 3.0]),
        "end": np.array([53.24, 80.09, 67.20]),
        "initial_fracs": [0.00, 0.20, 0.42, 0.62, 1.00],
        "add_fracs": [0.74, 0.86, 0.94],
        "background": "warm",
    },
    {
        "name": "case2_cool_blue",
        "title": "Chart 2: neutral to blue gamut boundary",
        "p0": (18.0, 24.0),
        "p1": (190.0, 120.0),
        "base": np.array([72.0, 2.0, -6.0]),
        "end": np.array([32.30, 79.19, -107.86]),
        "initial_fracs": [0.00, 0.18, 0.38, 0.58, 1.00],
        "add_fracs": [0.70, 0.83, 0.93],
        "background": "cool",
    },
    {
        "name": "case3_green_boundary",
        "title": "Chart 3: neutral to green gamut boundary",
        "p0": (22.0, 72.0),
        "p1": (186.0, 72.0),
        "base": np.array([74.0, -6.0, 8.0]),
        "end": np.array([87.73, -86.18, 83.18]),
        "initial_fracs": [0.00, 0.20, 0.40, 0.60, 0.74],
        "add_fracs": [0.85, 0.93, 1.00],
        "background": "green",
    },
]


def srgb_to_linear(c: np.ndarray) -> np.ndarray:
    return np.where(c > 0.04045, ((c + 0.055) / 1.055) ** 2.4, c / 12.92)


def linear_to_srgb(c: np.ndarray) -> np.ndarray:
    return np.where(c > 0.0031308, 1.055 * np.power(np.maximum(c, 0.0), 1 / 2.4) - 0.055, 12.92 * c)


def rgb_to_lab(rgb: np.ndarray) -> np.ndarray:
    """uint8/float sRGB array (...,3) -> CIE Lab under D65."""
    rgb = np.asarray(rgb, dtype=float)
    if rgb.size and np.nanmax(rgb) > 1.5:
        rgb = rgb / 255.0
    lin = srgb_to_linear(rgb)
    shape = lin.shape
    flat = lin.reshape(-1, 3)
    m = np.array(
        [
            [0.4124564, 0.3575761, 0.1804375],
            [0.2126729, 0.7151522, 0.0721750],
            [0.0193339, 0.1191920, 0.9503041],
        ]
    )
    xyz = flat @ m.T
    xyz /= np.array([0.95047, 1.0, 1.08883])
    eps = 216 / 24389
    kappa = 24389 / 27
    f = np.where(xyz > eps, np.cbrt(xyz), (kappa * xyz + 16) / 116)
    lab = np.empty_like(xyz)
    lab[:, 0] = 116 * f[:, 1] - 16
    lab[:, 1] = 500 * (f[:, 0] - f[:, 1])
    lab[:, 2] = 200 * (f[:, 1] - f[:, 2])
    return lab.reshape(shape)


def lab_to_rgb(lab: np.ndarray) -> np.ndarray:
    """CIE Lab -> float sRGB, values can be outside [0,1] for out-of-gamut."""
    lab = np.asarray(lab, dtype=float)
    shape = lab.shape
    flat = lab.reshape(-1, 3)
    fy = (flat[:, 0] + 16) / 116
    fx = flat[:, 1] / 500 + fy
    fz = fy - flat[:, 2] / 200
    eps = 216 / 24389
    kappa = 24389 / 27

    def inv_f(t: np.ndarray) -> np.ndarray:
        return np.where(t**3 > eps, t**3, (116 * t - 16) / kappa)

    xyz = np.column_stack([inv_f(fx) * 0.95047, inv_f(fy), inv_f(fz) * 1.08883])
    minv = np.array(
        [
            [3.2404542, -1.5371385, -0.4985314],
            [-0.9692660, 1.8760108, 0.0415560],
            [0.0556434, -0.2040259, 1.0572252],
        ]
    )
    lin = xyz @ minv.T
    srgb = linear_to_srgb(lin)
    return srgb.reshape(shape)


def lab_to_rgb_uint8(lab: np.ndarray) -> np.ndarray:
    raw = lab_to_rgb(lab)
    return np.clip(np.rint(raw * 255.0), 0, 255).astype(np.uint8)


def font(size: int = 16) -> ImageFont.ImageFont:
    for path in [
        "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
        "/usr/share/fonts/truetype/liberation2/LiberationSans-Regular.ttf",
    ]:
        if Path(path).exists():
            return ImageFont.truetype(path, size)
    return ImageFont.load_default()


def make_background(case: dict) -> np.ndarray:
    yy, xx = np.mgrid[0:H, 0:W].astype(float)
    nx = xx / (W - 1)
    ny = yy / (H - 1)
    base = case["base"]
    kind = case["background"]

    if kind == "warm":
        light = base[0] + 5.0 * nx + 2.5 * np.sin(math.pi * ny)
        a = base[1] + 7.0 * (nx - 0.5) + 2.0 * np.sin(2 * math.pi * ny)
        b = base[2] + 7.0 * (ny - 0.5)
    elif kind == "cool":
        light = base[0] + 4.0 * (1 - nx) + 4.0 * ny
        a = base[1] + 6.0 * np.sin(math.pi * nx) - 3.0 * (ny - 0.5)
        b = base[2] + 8.0 * (nx - 0.5) + 4.0 * np.sin(math.pi * ny)
    else:
        light = base[0] + 3.5 * nx - 3.0 * (ny - 0.5)
        a = base[1] + 7.0 * (ny - 0.5) + 2.5 * np.sin(2 * math.pi * nx)
        b = base[2] + 7.5 * (nx - 0.5)

    return np.stack([light, a, b], axis=-1)


def make_chart(case: dict) -> tuple[np.ndarray, dict]:
    bg_lab = make_background(case)
    yy, xx = np.mgrid[0:H, 0:W].astype(float)
    x0, y0 = case["p0"]
    x1, y1 = case["p1"]
    dx, dy = x1 - x0, y1 - y0
    length = math.hypot(dx, dy)
    u = ((xx - x0) * dx + (yy - y0) * dy) / (length * length)
    perp = np.abs(-(xx - x0) * dy + (yy - y0) * dx) / length
    mask = (u >= 0.0) & (u <= 1.0) & (perp <= PERP_HALF_WIDTH)

    ray_lab = case["base"][None, None, :] * (1.0 - u[..., None]) + case["end"][None, None, :] * u[..., None]
    lab = bg_lab.copy()
    lab[mask] = ray_lab[mask]
    rgb = lab_to_rgb_uint8(lab)

    geometry = {"u": u, "perp": perp, "mask": mask, "length": length}
    return rgb, geometry


def make_background_palette() -> np.ndarray:
    # Spacing 8 in each Lab coordinate bounds the ideal three-channel
    # nearest-center error to sqrt(3 * 4**2) ~= 6.9 Delta E.
    levels_l = [56.0, 64.0, 72.0, 80.0, 88.0]
    levels_ab = [-16.0, -8.0, 0.0, 8.0, 16.0]
    pts = []
    for l in levels_l:
        for a in levels_ab:
            for b in levels_ab:
                pts.append([l, a, b])
    return np.array(pts, dtype=float)


def ray_palette_lab(case: dict, fracs: list[float]) -> np.ndarray:
    frac = np.array(fracs, dtype=float)
    return case["base"][None, :] * (1 - frac[:, None]) + case["end"][None, :] * frac[:, None]


def build_palette_rgb(initial_lab: list[np.ndarray], added_lab: list[np.ndarray] | None = None) -> np.ndarray:
    labs = [make_background_palette()] + initial_lab
    if added_lab is not None:
        labs.extend(added_lab)
    lab = np.vstack(labs)
    rgb = lab_to_rgb_uint8(lab)
    # One RGB entry can correspond to two near-identical Lab entries.
    rgb = np.unique(rgb, axis=0)
    return rgb


def quantize(rgb: np.ndarray, palette_rgb: np.ndarray) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
    src_lab = rgb_to_lab(rgb)
    pal_lab = rgb_to_lab(palette_rgb)
    flat_src = src_lab.reshape(-1, 1, 3)
    # Chunk over pixels to keep temporary memory modest.
    idx = np.empty(src_lab.shape[:2], dtype=np.int32)
    flat_idx = idx.reshape(-1)
    chunk = 4096
    src_flat = src_lab.reshape(-1, 3)
    for start in range(0, len(src_flat), chunk):
        stop = min(start + chunk, len(src_flat))
        d2 = ((src_flat[start:stop, None, :] - pal_lab[None, :, :]) ** 2).sum(axis=2)
        flat_idx[start:stop] = d2.argmin(axis=1)
    q_rgb = palette_rgb[idx]
    q_lab = rgb_to_lab(q_rgb)
    err = np.linalg.norm(src_lab - q_lab, axis=2)
    return err, idx, q_rgb


def point_at_u(case: dict, u: float) -> tuple[float, float]:
    x0, y0 = case["p0"]
    x1, y1 = case["p1"]
    return x0 + u * (x1 - x0), y0 + u * (y1 - y0)


def candidate_set(case: dict, fracs: list[float]) -> list[dict]:
    d = np.linalg.norm(case["end"] - case["base"])
    fracs = sorted(float(v) for v in fracs)
    out: list[dict] = []
    for i, (u0, u1) in enumerate(zip(fracs[:-1], fracs[1:])):
        u = 0.5 * (u0 + u1)
        out.append(
            {
                "u": u,
                "kind": "center_midpoint",
                "pair": [i, i + 1],
                "theoretical_delta_e": d * (u1 - u0) / 2.0,
                "xy": point_at_u(case, u),
            }
        )
    for u, label in [(0.0, "gamut_or_trajectory_boundary"), (1.0, "gamut_boundary")]:
        nearest = min(fracs, key=lambda z: abs(z - u))
        out.append(
            {
                "u": u,
                "kind": label,
                "pair": None,
                "theoretical_delta_e": d * abs(u - nearest),
                "xy": point_at_u(case, u),
            }
        )
    return out


def bins_for_error(err: np.ndarray) -> np.ndarray:
    return np.digitize(err, EDGES[1:-1], right=False).astype(np.uint8)


def heatmap_image(err: np.ndarray) -> Image.Image:
    b = bins_for_error(err)
    rgb = HEAT_COLORS[b]
    img = Image.fromarray(rgb, "RGB").resize((W * SCALE, H * SCALE), Image.Resampling.NEAREST)
    return img


def add_heatmap_legend(img: Image.Image, counts: list[int], max_de: float, stage: str) -> Image.Image:
    panel_w = 330
    canvas = Image.new("RGB", (img.width + panel_w, img.height), "white")
    canvas.paste(img, (0, 0))
    draw = ImageDraw.Draw(canvas)
    f_title = font(22)
    f = font(17)
    f_small = font(15)
    x = img.width + 18
    y = 18
    draw.text((x, y), f"{stage} heatmap: 8 fixed bins", fill="#111111", font=f_title)
    y += 38
    draw.text((x, y), "Delta E76 bin / pixel count", fill="#333333", font=f)
    y += 30
    total = sum(counts)
    for i, label in enumerate(BIN_LABELS):
        color = tuple(int(v) for v in HEAT_COLORS[i])
        draw.rectangle([x, y, x + 34, y + 24], fill=color, outline="#555555")
        draw.text((x + 44, y + 2), label, fill="#111111", font=f)
        c = counts[i]
        pct = 100.0 * c / total
        draw.text((x + 142, y + 2), f"{c:,} ({pct:.2f}%)", fill="#111111", font=f_small)
        y += 31
    y += 8
    occupied = sum(c > 0 for c in counts)
    draw.text((x, y), f"Nonempty buckets: {occupied}/8", fill="#111111", font=f)
    y += 28
    draw.text((x, y), f"Measured max Delta E: {max_de:.2f}", fill="#111111", font=f)
    return canvas


def draw_overlay(rgb: np.ndarray, case: dict, candidates: list[dict], pred: dict, mx: int, my: int, path: Path) -> None:
    img = Image.fromarray(rgb, "RGB").resize((W * SCALE, H * SCALE), Image.Resampling.NEAREST)
    draw = ImageDraw.Draw(img)

    def S(p: tuple[float, float]) -> tuple[float, float]:
        return p[0] * SCALE, p[1] * SCALE

    # Candidate finite set: blue diamonds for midpoints, white squares boundary.
    for cand in candidates:
        x, y = S(cand["xy"])
        if cand["kind"] == "center_midpoint":
            r = 6 * SCALE / 2
            draw.line([(x, y - r), (x + r, y), (x, y + r), (x - r, y), (x, y - r)], fill=(0, 102, 255), width=3)
        else:
            r = 5
            draw.rectangle([x - r, y - r, x + r, y + r], outline=(255, 255, 255), width=3)

    px, py = S(pred["xy"])
    # Predicted global maximum: yellow circle.
    r = 9 * SCALE / 2
    draw.ellipse([px - r, py - r, px + r, py + r], outline=(255, 230, 0), width=4)
    if px > W * SCALE * 0.78:
        p_label_xy = (px - 178, py - 38)
    else:
        p_label_xy = (px + 12, py - 24)
    draw.text(p_label_xy, "P predicted", fill=(255, 230, 0), font=font(18), stroke_width=2, stroke_fill=(0, 0, 0))

    x, y = mx * SCALE + SCALE / 2, my * SCALE + SCALE / 2
    r = 8 * SCALE / 2
    draw.ellipse([x - r, y - r, x + r, y + r], outline=(255, 0, 255), width=4)
    draw.line([px, py, x, y], fill=(0, 255, 255), width=2)
    if x > W * SCALE * 0.78:
        m_label_xy = (x - 178, y + 12)
    else:
        m_label_xy = (x + 12, y + 8)
    draw.text(m_label_xy, "M measured", fill=(255, 0, 255), font=font(18), stroke_width=2, stroke_fill=(0, 0, 0))

    # Coordinate box in image coordinates.
    dx = mx - pred["xy"][0]
    dy = my - pred["xy"][1]
    dist = math.hypot(dx, dy)
    info = f"P=({pred['xy'][0]:.2f},{pred['xy'][1]:.2f})  M=({mx},{my})  d=({dx:+.2f},{dy:+.2f})  |d|={dist:.2f}px"
    legend = "diamond = midpoint candidate    square = boundary candidate"
    panel_h = 66
    canvas = Image.new("RGB", (img.width, img.height + panel_h), "white")
    canvas.paste(img, (0, 0))
    d2 = ImageDraw.Draw(canvas)
    d2.text((12, img.height + 8), info, fill="#111111", font=font(18))
    d2.text((12, img.height + 38), legend, fill="#444444", font=font(15))
    canvas.save(path)


def counts(err: np.ndarray) -> list[int]:
    b = bins_for_error(err)
    return [int(np.sum(b == i)) for i in range(len(BIN_LABELS))]


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "images").mkdir(exist_ok=True)
    (OUT / "heatmaps").mkdir(exist_ok=True)

    bg_palette_lab = make_background_palette()
    initial_ray_labs = [ray_palette_lab(c, c["initial_fracs"]) for c in CASES]
    initial_palette = build_palette_rgb(initial_ray_labs)

    refined_ray_labs = []
    for c in CASES:
        refined_fracs = sorted(c["initial_fracs"] + c["add_fracs"])
        refined_ray_labs.append(ray_palette_lab(c, refined_fracs))
    refined_palette = build_palette_rgb(initial_ray_labs, refined_ray_labs)
    np.savetxt(OUT / "palette_initial_rgb.csv", initial_palette, delimiter=",", header="r,g,b", comments="", fmt="%d")
    np.savetxt(OUT / "palette_refined_rgb.csv", refined_palette, delimiter=",", header="r,g,b", comments="", fmt="%d")

    report = {
        "metric": "CIEDE76 / Delta E*ab (Euclidean Lab D65)",
        "bins": BIN_LABELS,
        "bin_edges": [None if math.isinf(v) else v for v in EDGES],
        "palette_rgb_initial_size": int(len(initial_palette)),
        "palette_rgb_refined_size": int(len(refined_palette)),
        "background_palette_size": int(len(np.unique(lab_to_rgb_uint8(bg_palette_lab), axis=0))),
        "cases": [],
    }

    table_rows = []
    bucket_rows = []

    for case, initial_ray_lab, refined_ray_lab in zip(CASES, initial_ray_labs, refined_ray_labs):
        rgb, geom = make_chart(case)
        name = case["name"]
        Image.fromarray(rgb, "RGB").save(OUT / "images" / f"{name}_source.png")

        err0, idx0, q0 = quantize(rgb, initial_palette)
        err1, idx1, q1 = quantize(rgb, refined_palette)
        Image.fromarray(q0, "RGB").save(OUT / "images" / f"{name}_quantized_before.png")
        Image.fromarray(q1, "RGB").save(OUT / "images" / f"{name}_quantized_after.png")

        candidates = candidate_set(case, case["initial_fracs"])
        pred = max(candidates, key=lambda z: z["theoretical_delta_e"])
        my, mx = np.unravel_index(int(np.argmax(err0)), err0.shape)
        measured_max = float(err0[my, mx])
        dx = mx - pred["xy"][0]
        dy = my - pred["xy"][1]
        dist = math.hypot(dx, dy)

        # Useful diagnostics: worst point restricted to the calibration ray.
        ray_err = np.where(geom["mask"], err0, -1.0)
        rmy, rmx = np.unravel_index(int(np.argmax(ray_err)), ray_err.shape)
        ray_dist = math.hypot(rmx - pred["xy"][0], rmy - pred["xy"][1])

        draw_overlay(rgb, case, candidates, pred, int(mx), int(my), OUT / "images" / f"{name}_predicted_vs_measured.png")

        hb = heatmap_image(err0)
        c0 = counts(err0)
        hb_legend = add_heatmap_legend(hb, c0, float(err0.max()), "Before refinement")
        hb_legend.save(OUT / "heatmaps" / f"{name}_heatmap_before.png")

        ha = heatmap_image(err1)
        c1 = counts(err1)
        ha_legend = add_heatmap_legend(ha, c1, float(err1.max()), "After boundary refinement")
        ha_legend.save(OUT / "heatmaps" / f"{name}_heatmap_after.png")

        # Side-by-side compact figure.
        overlay = Image.open(OUT / "images" / f"{name}_predicted_vs_measured.png")
        combo_h = overlay.height
        heat_before = Image.open(OUT / "heatmaps" / f"{name}_heatmap_before.png")
        heat_after = Image.open(OUT / "heatmaps" / f"{name}_heatmap_after.png")
        # Resize heatmap legends to overlay height if needed.
        def fit_height(im: Image.Image) -> Image.Image:
            w = round(im.width * combo_h / im.height)
            return im.resize((w, combo_h), Image.Resampling.LANCZOS)

        heat_before = fit_height(heat_before)
        heat_after = fit_height(heat_after)
        combo = Image.new("RGB", (overlay.width + heat_before.width + heat_after.width, combo_h), "white")
        combo.paste(overlay, (0, 0))
        combo.paste(heat_before, (overlay.width, 0))
        combo.paste(heat_after, (overlay.width + heat_before.width, 0))
        combo.save(OUT / f"{name}_figure.png")

        case_report = {
            "title": case["title"],
            "image_size": [W, H],
            "initial_fracs": case["initial_fracs"],
            "added_fracs": case["add_fracs"],
            "refined_fracs": sorted(case["initial_fracs"] + case["add_fracs"]),
            "candidate_count": len(candidates),
            "candidates": candidates,
            "predicted": {
                **pred,
                "xy": list(pred["xy"]),
                "rounded_xy": [int(round(pred["xy"][0])), int(round(pred["xy"][1]))],
            },
            "measured_before": {"x": int(mx), "y": int(my), "delta_e": measured_max},
            "coordinate_delta": {"dx": dx, "dy": dy, "euclidean_px": dist, "within_2px": bool(dist <= 2.0)},
            "ray_restricted_measured": {
                "x": int(rmx),
                "y": int(rmy),
                "delta_e": float(err0[rmy, rmx]),
                "euclidean_px_to_prediction": ray_dist,
            },
            "max_delta_e_before": float(err0.max()),
            "max_delta_e_after": float(err1.max()),
            "mean_delta_e_before": float(err0.mean()),
            "mean_delta_e_after": float(err1.mean()),
            "p95_delta_e_before": float(np.percentile(err0, 95)),
            "p95_delta_e_after": float(np.percentile(err1, 95)),
            "buckets_before": c0,
            "buckets_after": c1,
            "nonempty_buckets_before": sum(v > 0 for v in c0),
            "nonempty_buckets_after": sum(v > 0 for v in c1),
        }
        report["cases"].append(case_report)

        table_rows.append(
            {
                "image": name,
                "pred_x": f"{pred['xy'][0]:.2f}",
                "pred_y": f"{pred['xy'][1]:.2f}",
                "meas_x": int(mx),
                "meas_y": int(my),
                "dx": f"{dx:+.2f}",
                "dy": f"{dy:+.2f}",
                "distance_px": f"{dist:.2f}",
                "within_2px": dist <= 2.0,
                "pred_kind": pred["kind"],
                "de_before_at_measured": f"{measured_max:.2f}",
                "max_de_before": f"{err0.max():.2f}",
                "max_de_after": f"{err1.max():.2f}",
            }
        )
        for i, label in enumerate(BIN_LABELS):
            bucket_rows.append(
                {
                    "image": name,
                    "bin": i,
                    "range": label,
                    "before": c0[i],
                    "after": c1[i],
                    "change_after_minus_before": c1[i] - c0[i],
                }
            )

    with (OUT / "metrics.json").open("w", encoding="utf-8") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)

    with (OUT / "coordinates.csv").open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(table_rows[0].keys()))
        writer.writeheader()
        writer.writerows(table_rows)

    with (OUT / "bucket_counts.csv").open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(bucket_rows[0].keys()))
        writer.writeheader()
        writer.writerows(bucket_rows)

    print(json.dumps({
        "out": str(OUT),
        "palette": [report["palette_rgb_initial_size"], report["palette_rgb_refined_size"]],
        "rows": table_rows,
    }, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
