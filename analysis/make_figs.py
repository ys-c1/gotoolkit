"""
Render figures with PIL (no matplotlib):
  fig1_edgezoom_<image>.png : 3 supports x (original / reconstructed zoom),
                              red bracket marks the r+2s influence band,
                              pixel grid overlaid so widths can be counted
  fig_esf.png               : chroma error ESF profiles per support
  fig_tradeoff.png          : luma RMSE vs fringe width with fitted slope
  fig_widthcheck.png        : measured vs r+2s bar comparison
"""
import json
import sys
import numpy as np
from PIL import Image, ImageDraw, ImageFont

sys.path.insert(0, "/home/ys/feedback/uidreczz28dpNBzBitz/analysis")
from chroma_2d import IMAGES, rgb2ycbcr, subsample_rgb
from measure import influence_mask_1d
from experiment import edge_columns_vertical, SUPPORTS, R, EPS, local_lipschitz

FIG = "/home/ys/feedback/uidreczz28dpNBzBitz/analysis/figs"
BLUE = (42, 120, 214)
ORANGE = (235, 104, 52)
AQUA = (27, 175, 122)
RED = (227, 73, 72)
INK = (11, 11, 11)
MUTED = (137, 135, 129)
GRID = (225, 224, 217)
SURF = (252, 252, 251)
SCOL = [BLUE, ORANGE, AQUA]


def font(size, bold=False):
    path = ("/usr/share/fonts/truetype/ubuntu/"
            + ("Ubuntu-B.ttf" if bold else "Ubuntu-R.ttf"))
    return ImageFont.truetype(path, size)


def upscale(arr, k):
    im = Image.fromarray(arr)
    return im.resize((arr.shape[1] * k, arr.shape[0] * k), Image.NEAREST)


def text(draw, xy, s, f, fill=INK):
    draw.text(xy, s, font=f, fill=fill)


def edge_zoom_figure(name):
    rgb = IMAGES[name]()
    ycc = rgb2ycbcr(rgb)
    edge, erow = edge_columns_vertical(ycc[..., 1])
    half = 10
    a, b = edge - half, edge + half
    r0 = max(erow - half, 0)
    r1 = min(erow + half, rgb.shape[0])
    k = 20                                   # pixel -> screen pixels
    pad = 70
    title_h = 50
    row_h = (r1 - r0) * k
    cellw = (b - a) * k
    W = pad * 2 + cellw * 2 + 40
    H = title_h + (row_h + 78) * len(SUPPORTS) + 20
    canvas = Image.new("RGB", (W, H), SURF)
    d = ImageDraw.Draw(canvas)
    f_t = font(23, True)
    f = font(17)
    fs = font(15)
    text(d, (pad, 14),
         f"chroma fringe at the {name} edge  (r={R}, tent kernel; "
         f"red bracket = r+2s influence band)", f_t)
    for row, s in enumerate(SUPPORTS):
        rec_rgb, _ = subsample_rgb(rgb, R, s, kind="tent")
        y0 = title_h + row * (row_h + 78)
        for col, (imarr, label) in enumerate(
                ((rgb, "original"), (rec_rgb, f"reconstructed, s={s}"))):
            x0 = pad + col * (cellw + 40)
            crop = np.clip(imarr, 0, 255).astype(np.uint8)[r0:r1, a:b]
            canvas.paste(upscale(crop, k), (x0, y0))
            for gx in range(0, cellw + 1, k):
                d.line([(x0 + gx, y0), (x0 + gx, y0 + row_h)],
                       fill=GRID, width=1)
            for gy in range(0, row_h + 1, k):
                d.line([(x0, y0 + gy), (x0 + cellw, y0 + gy)],
                       fill=GRID, width=1)
            d.rectangle([x0, y0, x0 + cellw - 1, y0 + row_h - 1],
                        outline=MUTED, width=1)
            text(d, (x0, y0 + row_h + 8), label, f)
        # influence band bracket under the reconstructed panel
        mask, pL, pR = influence_mask_1d(float(edge), R, s, rgb.shape[1])
        xr = pad + (cellw + 40)
        bx0 = xr + (pL - s - a) * k
        bx1 = xr + (pR + s - a) * k
        by = y0 + row_h + 38
        d.line([(bx0, by), (bx1, by)], fill=RED, width=3)
        for bx in (bx0, bx1):
            d.line([(bx, by - 6), (bx, by + 6)], fill=RED, width=3)
        span = (pR + s) - (pL - s)
        text(d, ((bx0 + bx1) // 2 - 55, by + 10),
             f"W = r+2s = {span:.0f} px", fs, fill=RED)
    canvas.save(f"{FIG}/fig1_edgezoom_{name}.png")


def _nice_ticks(lo, hi, n=5):
    span = hi - lo
    raw = span / n
    mag = 10 ** np.floor(np.log10(raw))
    for step in (1, 2, 2.5, 5, 10):
        if step * mag >= raw:
            tick = step * mag
            break
    start = np.ceil(lo / tick) * tick
    ticks = []
    v = start
    while v <= hi + 1e-9:
        ticks.append(round(v, 6))
        v += tick
    return ticks


def _line_chart(path, series, xlabel, ylabel, title, xlim, ylim,
                marks=False, hlines=None, slope_labels=None):
    """series: list of (label, color, xs, ys)."""
    W, H = 980, 580
    x0, y0, w, h = 140, 100, 760, 350
    im = Image.new("RGB", (W, H), SURF)
    d = ImageDraw.Draw(im)
    f_t, f, fs = font(22, True), font(16), font(14)
    text(d, (x0, y0 - 46), title, f_t)
    text(d, (x0 + w // 2 - 60, y0 + h + 46), xlabel, f)
    text(d, (x0, y0 - 22), ylabel, fs, fill=MUTED)
    d.rectangle([x0, y0, x0 + w, y0 + h], outline=MUTED, width=1)
    xlo, xhi = xlim
    ylo, yhi = ylim

    def px(x):
        return x0 + (x - xlo) / (xhi - xlo) * w

    def py(y):
        return y0 + h - (y - ylo) / (yhi - ylo) * h

    for tv in _nice_ticks(ylo, yhi):
        if tv < ylo - 1e-9 or tv > yhi + 1e-9:
            continue
        gy = py(tv)
        d.line([(x0, gy), (x0 + w, gy)], fill=GRID, width=1)
        text(d, (x0 - 52, gy - 9), f"{tv:g}", fs, fill=MUTED)
    for tv in _nice_ticks(xlo, xhi, 8):
        if tv < xlo - 1e-9 or tv > xhi + 1e-9:
            continue
        gx = px(tv)
        d.line([(gx, y0 + h), (gx, y0 + h + 5)], fill=MUTED, width=1)
        text(d, (gx - 8, y0 + h + 10), f"{tv:g}", fs, fill=MUTED)
    if hlines:
        for hy, col, lab in hlines:
            yy = py(hy)
            d.line([(x0, yy), (x0 + w, yy)], fill=col, width=1)
            text(d, (x0 + w - 170, yy - 18), lab, fs, fill=col)
    for label, col, xs, ys in series:
        pts = [(px(x), py(y)) for x, y in zip(xs, ys)]
        d.line(pts, fill=col, width=3)
        if marks:
            for p in pts:
                d.ellipse([p[0] - 5, p[1] - 5, p[0] + 5, p[1] + 5],
                          outline=col, width=2, fill=SURF)
    lx, ly = x0 + w - 250, y0 + 14
    for i, (label, col, _, _) in enumerate(series):
        yy = ly + i * 26
        d.line([(lx, yy), (lx + 26, yy)], fill=col, width=3)
        text(d, (lx + 34, yy - 10), label, f)
    if slope_labels:
        for i, (col, txt) in enumerate(slope_labels):
            text(d, (x0 + 16, y0 + h - 30 - i * 22), txt, fs, fill=col)
    im.save(path)


def esf_figure():
    """Chroma-error ESF across the vertical hard edge, one line per support."""
    rgb = IMAGES["vertical"]()
    ycc = rgb2ycbcr(rgb)
    edge, _ = edge_columns_vertical(ycc[..., 1])
    half = 12
    series = []
    for s, col in zip(SUPPORTS, SCOL):
        _, rec = subsample_rgb(rgb, R, s, kind="tent")
        err = np.sqrt(((rec[..., 1] - ycc[..., 1]) ** 2
                       + (rec[..., 2] - ycc[..., 2]) ** 2).mean(axis=0))
        xs = np.arange(edge - half, edge + half) - edge
        ys = err[edge - half:edge + half]
        series.append((f"s={s} (W={R+2*s:.0f}px)", col, xs, ys))
    # 2% threshold of the edge chroma amplitude
    amp = float(np.sqrt(((ycc[:, 1:, 1] - ycc[:, :-1, 1]) ** 2
                         + (ycc[:, 1:, 2] - ycc[:, :-1, 2]) ** 2).max()))
    _line_chart(f"{FIG}/fig_esf.png", series,
                "distance from true edge (px)", "chroma error (CbCr units)",
                "chroma-error ESF across the edge (r=2, tent)",
                (-half, half - 1), (0, amp * 1.05),
                hlines=[(0.02 * amp, RED, f"2% threshold = {0.02*amp:.1f}")])


def tradeoff_figure():
    data = json.load(open(
        "/home/ys/feedback/uidreczz28dpNBzBitz/analysis/results.json"))
    series = []
    slope_labels = []
    for i, name in enumerate(IMAGES):
        rows = [r for r in data["rows"] if r["image"] == name]
        xs = [r["infl_span"] for r in rows]
        ys = [r["luma_rmse"] for r in rows]
        beta = np.polyfit(xs, ys, 1)[0]
        # normalized RMSE: fraction of the narrowest-support (best fringe) loss
        base = ys[0]
        yn = [v / base for v in ys]
        col = SCOL[i]
        series.append((f"{name} (RMSE {base:.1f}->{ys[-1]:.1f})",
                       col, xs, yn))
        slope_labels.append(
            (col, f"{name}: C = {beta:.2f} luma-RMSE per 1 px fringe "
                  f"narrowed"))
    _line_chart(f"{FIG}/fig_tradeoff.png", series,
                "fringe width W = r+2s (px)",
                "luma RMSE / RMSE at narrowest kernel",
                "trade-off: narrowing the kernel widens luma error (r=2)",
                (6.5, 9.5), (0.95, 1.18), marks=True,
                slope_labels=slope_labels)


def width_check_figure():
    data = json.load(open(
        "/home/ys/feedback/uidreczz28dpNBzBitz/analysis/results.json"))
    rows = data["rows"]
    n = len(rows)
    W, H = 1080, 580
    x0, y0, w, h = 140, 100, 880, 360
    im = Image.new("RGB", (W, H), SURF)
    d = ImageDraw.Draw(im)
    f_t, f, fs = font(22, True), font(15), font(13)
    text(d, (x0, y0 - 46),
         "measured vs predicted fringe width  (|diff| <= 1 px required)", f_t)
    text(d, (x0, y0 - 22), "fringe width (px)", fs, fill=MUTED)
    ylo, yhi = 0.0, 10.5

    def py(y):
        return y0 + h - (y - ylo) / (yhi - ylo) * h

    for tv in _nice_ticks(ylo, yhi):
        gy = py(tv)
        d.line([(x0, gy), (x0 + w, gy)], fill=GRID, width=1)
        text(d, (x0 - 40, gy - 9), f"{tv:g}", fs, fill=MUTED)
    d.line([(x0, py(0)), (x0 + w, py(0))], fill=MUTED, width=1)
    group = w / n
    bw = group * 0.30
    for i, r in enumerate(rows):
        gx = x0 + group * (i + 0.5)
        for j, (val, col, lab) in enumerate(
                ((r["pred_width"], BLUE, "pred r+2s"),
                 (r["infl_span"], ORANGE, "measured"))):
            bx = gx + (j - 0.5) * bw * 1.25 - bw / 2
            top = py(val)
            d.rounded_rectangle([bx, top, bx + bw, py(0)],
                                radius=4, fill=col)
            text(d, (bx + bw / 2 - 12, top - 20), f"{val:g}", fs, fill=col)
        diff = abs(r["infl_span"] - r["pred_width"])
        text(d, (gx - 26, py(0) + 8),
             f"{r['image'][:3]} s={r['s']}", fs)
        text(d, (gx - 30, py(0) + 26),
             f"diff {diff:g}px", fs,
             fill=AQUA if diff <= 1.0 else RED)
    # legend
    for j, (col, lab) in enumerate(((BLUE, "predicted r+2s"),
                                    (ORANGE, "measured band"))):
        lx = x0 + w - 250 + j * 130
        d.rounded_rectangle([lx, y0 - 70, lx + 18, y0 - 54], radius=3,
                            fill=col)
        text(d, (lx + 24, y0 - 72), lab, f)
    im.save(f"{FIG}/fig_widthcheck.png")


if __name__ == "__main__":
    for name in IMAGES:
        edge_zoom_figure(name)
        print("saved edgezoom", name)
    esf_figure()
    print("saved esf")
    tradeoff_figure()
    print("saved tradeoff")
    width_check_figure()
    print("saved widthcheck")
