# -*- coding: utf-8 -*-
"""2D 图像实验：补零 vs 对称延拓的高斯通道重建。

输出：
- 测试图、两种延拓各层数重建图、误差图、边界放大图（PNG）；
- 暗边宽度随层数的实测/理论对照（CSV）；
- 最外两列 vs 内部像素的分区误差统计（CSV）。
"""

import csv
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import numpy as np
from PIL import Image, ImageDraw, ImageFont

from pyramid import gaussian_recon, laplacian_recon

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "output")
FIG = os.path.join(OUT, "figures")
DAT = os.path.join(OUT, "data")
os.makedirs(FIG, exist_ok=True)
os.makedirs(DAT, exist_ok=True)

N = 128
LEVELS = [1, 2, 3, 4]


def make_test_image(n):
    """平滑为主、含纹理与亮暗结构的灰度图；四周留出平坦亮区便于观察暗边。"""
    yy, xx = np.mgrid[0:n, 0:n].astype(np.float64)
    img = np.full((n, n), 200.0)
    # 两个高斯斑（一亮一暗）
    img += 70 * np.exp(-(((xx - 42) ** 2 + (yy - 46) ** 2) / (2 * 16.0 ** 2)))
    img -= 55 * np.exp(-(((xx - 88) ** 2 + (yy - 80) ** 2) / (2 * 11.0 ** 2)))
    # 缓变斜坡
    img += 0.25 * (xx + yy)
    # 弱正弦纹理
    img += 6.0 * np.sin(xx / 4.5) * np.sin(yy / 6.0)
    return np.clip(img, 0, 255)


def save_gray(arr, path):
    Image.fromarray(np.clip(arr + 0.5, 0, 255).astype(np.uint8), "L").save(path)


def border_widths(err, tol=1e-9):
    """平坦常数场上四边暗边（有符号负误差）的连续像素宽度。

    每条边从外缘向内逐列/逐行计数，取该线上所有像素均为负误差的连续长度。
    """
    e = err

    def run(mask_line):
        w = 0
        for v in mask_line:
            if v:
                w += 1
            else:
                break
        return w

    neg = e < -tol
    top = run([neg[i, :].all() for i in range(e.shape[0])])
    bottom = run([neg[e.shape[0] - 1 - i, :].all() for i in range(e.shape[0])])
    left = run([neg[:, i].all() for i in range(e.shape[1])])
    right = run([neg[:, e.shape[1] - 1 - i].all() for i in range(e.shape[1])])
    return top, bottom, left, right


def zoom_crop(img, box, scale=6):
    """边界区域最近邻放大，加网格线与坐标标注。"""
    x0, y0, x1, y1 = box
    crop = img[y0:y1, x0:x1]
    h, w = crop.shape
    big = Image.fromarray(np.clip(crop + 0.5, 0, 255).astype(np.uint8), "L")
    big = big.resize((w * scale, h * scale), Image.NEAREST)
    draw = ImageDraw.Draw(big)
    for i in range(w + 1):
        draw.line([(i * scale, 0), (i * scale, h * scale)], fill=90)
    for j in range(h + 1):
        draw.line([(0, j * scale), (w * scale, j * scale)], fill=90)
    return big


def main():
    img = make_test_image(N)
    save_gray(img, os.path.join(FIG, "00_original.png"))

    # 平坦常数场：专门用来测暗边宽度（排除图像内容干扰）
    flat = np.full((N, N), 200.0)

    width_rows = []
    err_rows = []
    edge_rows = []
    # 对称延拓外两列的 ℓ1 界常数（来自 bounds.py 的精确分数，L=1..4）
    edge_C = {1: (0.25, 0.875), 2: (0.6875, 1.359375),
              3: (1.515625, 2.064453125), 4: (3.13671875, 3.533935546875)}

    for mode, tag in (("zero", "zero_pad"), ("sym", "symmetric")):
        for L in LEVELS:
            rec = gaussian_recon(img, L, mode)
            err = rec - img
            save_gray(rec, os.path.join(FIG, f"01_recon_{tag}_L{L}.png"))
            # 误差图：128 灰为零误差，偏暗=负（变暗），偏亮=正
            vis = np.clip(128 + err * 3, 0, 255)
            save_gray(vis, os.path.join(FIG, f"02_errmap_{tag}_L{L}.png"))

            # 暗边宽度（平坦场）
            rec_flat = gaussian_recon(flat, L, mode)
            tb, bb, lb, rb = border_widths(rec_flat - flat)
            theory = 2 ** L
            width_rows.append([tag, L, tb, bb, lb, rb, theory])

            # 分区误差（真实图像）
            # 最外两列：仅左缘第 0、1 列（用户指定口径），四角不计入两次
            outer2 = err[:, 0:2]
            band = 2 ** L
            interior = err[band:-band, band:-band]
            # 理论界对照需要图像的 G（最大一阶差分，取水平/垂直较大者）
            gx = np.abs(np.diff(img, axis=1)).max()
            gy = np.abs(np.diff(img, axis=0)).max()
            G = max(gx, gy)
            err_rows.append([
                tag, L,
                np.abs(outer2).max(), np.abs(outer2).mean(),
                outer2.mean(),
                np.abs(interior).max(), np.abs(interior).mean(),
                np.abs(err).max(), G,
            ])

            # 逐列明细：第 0、1 列各自的误差，及对称模式理论界 C·G
            c0, c1 = edge_C[L]
            for col, C in ((0, c0), (1, c1)):
                col_err = err[:, col]
                edge_rows.append([
                    tag, L, col,
                    np.abs(col_err).max(), np.abs(col_err).mean(),
                    col_err.mean(), C * G if mode == "sym" else "",
                ])

    # 暗边宽度表
    with open(os.path.join(DAT, "border_widths.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["mode", "L", "top", "bottom", "left", "right", "theory_2^L"])
        w.writerows(width_rows)

    # 分区误差表
    with open(os.path.join(DAT, "region_errors.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["mode", "L", "outer2_max_abs", "outer2_mean_abs",
                    "outer2_signed_mean", "interior_max_abs", "interior_mean_abs",
                    "global_max_abs", "image_G_max_first_diff"])
        w.writerows(err_rows)

    with open(os.path.join(DAT, "edge_two_columns.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["mode", "L", "column", "max_abs", "mean_abs", "signed_mean",
                    "sym_theory_bound_C*G"])
        w.writerows(edge_rows)

    # 边界放大图：左上角 16×16，两种延拓 L=3，与原图并列
    box = (0, 0, 16, 16)
    panels = [
        ("original", zoom_crop(img, box)),
        ("zero_pad L=3", zoom_crop(gaussian_recon(img, 3, "zero"), box)),
        ("symmetric L=3", zoom_crop(gaussian_recon(img, 3, "sym"), box)),
    ]
    pad = 8
    label_h = 30
    pw = panels[0][1].width
    ph = panels[0][1].height
    sheet = Image.new("L", (pw * 3 + pad * 4, ph + label_h + pad * 2), 255)
    draw = ImageDraw.Draw(sheet)
    try:
        lbl_font = ImageFont.truetype(
            "/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf", 18)
    except Exception:
        lbl_font = ImageFont.load_default()
    labels = ["原图", "补零 L=3", "对称 L=3"]
    for k, ((name, im), label) in enumerate(zip(panels, labels)):
        x = pad + k * (pw + pad)
        sheet.paste(im, (x, label_h))
        draw.text((x + 4, 5), label, fill=0, font=lbl_font)
    sheet.save(os.path.join(FIG, "03_zoom_top_left_L3.png"))

    # 两种延拓各自单独的边界放大图（满足"各存一张"）
    zoom_crop(gaussian_recon(img, 3, "zero"), box).save(
        os.path.join(FIG, "03a_zoom_zero_pad_L3.png"))
    zoom_crop(gaussian_recon(img, 3, "sym"), box).save(
        os.path.join(FIG, "03b_zoom_symmetric_L3.png"))

    # 拉普拉斯完整重建对照（应精确）
    lap_rows = []
    for mode in ("zero", "sym"):
        for L in LEVELS:
            r = laplacian_recon(img, L, mode)
            lap_rows.append([mode, L, np.abs(r - img).max()])
    with open(os.path.join(DAT, "laplacian_exact.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["mode", "L", "max_abs_recon_error"])
        w.writerows(lap_rows)

    # 控制台简报
    print("暗边宽度（平坦场，理论 2^L）:")
    for r in width_rows:
        print("  ", r)
    print("\n分区误差:")
    for r in err_rows:
        print("  ", r)
    print("\n拉普拉斯完整重建最大误差:")
    for r in lap_rows:
        print("  ", r)


if __name__ == "__main__":
    main()
