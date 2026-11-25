# -*- coding: utf-8 -*-
"""生成两张定量图（纯 PIL 手绘，无 matplotlib）：
04_width_vs_levels.png —— 暗边宽度随层数变化，理论 2^L 等式对照；
05_boundary_profile.png —— 平坦场左缘剖面，补零 vs 对称，多层数衰减曲线。
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import numpy as np
from PIL import Image, ImageDraw, ImageFont

from pyramid import gaussian_recon

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "output")
FIG = os.path.join(OUT, "figures")

# 中性配色（深墨蓝 / 暖橙 / 灰）
C_ZERO = (38, 70, 110)
C_SYM = (200, 110, 40)
C_THEORY = (140, 140, 140)
C_INK = (40, 40, 40)
C_GRID = (225, 225, 225)
C_SURF = (255, 255, 255)


def font(size=16):
    cands = [
        "/home/ys/.local/share/fonts/NotoSansCJKsc-Regular.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    ]
    for p in cands:
        if os.path.exists(p):
            return ImageFont.truetype(p, size)
    return ImageFont.load_default()


def axes(draw, x0, y0, w, h, xlab, ylab, title, fnt):
    draw.rectangle([x0, y0, x0 + w, y0 + h], outline=C_INK, width=2)
    draw.text((x0, y0 - 32), title, fill=C_INK, font=fnt)
    draw.text((x0 + w // 2 - 40, y0 + h + 14), xlab, fill=C_INK, font=fnt)
    # y 轴标签逐字竖排，避免重叠
    yy = y0 + h // 2 - len(ylab) * 11
    for ch in ylab:
        draw.text((x0 - 52, yy), ch, fill=C_INK, font=fnt)
        yy += 22


def plot_widths():
    N = 128
    flat = np.full((N, N), 200.0)
    Ls = list(range(1, 7))
    w_zero, w_sym = [], []
    for L in Ls:
        ez = gaussian_recon(flat, L, "zero") - flat
        es = gaussian_recon(flat, L, "sym") - flat
        # 左缘宽度（主等式口径）
        wz = 0
        while wz < N and (ez[:, wz] < -1e-9).all():
            wz += 1
        ws = 0
        while ws < N and (es[:, ws] < -1e-9).all():
            ws += 1
        w_zero.append(wz)
        w_sym.append(ws)

    W, H = 800, 480
    img = Image.new("RGB", (W, H), C_SURF)
    d = ImageDraw.Draw(img)
    fnt = font(16)
    x0, y0, w, h = 100, 80, 620, 300
    axes(d, x0, y0, w, h, "层数 L", "左缘暗边宽度（像素）",
         "暗边宽度随层数变化（平坦常数场，N=128）", fnt)

    xmax, ymax = 6.5, 70
    def X(v): return x0 + (v - 0.5) / (xmax - 0.5) * w
    def Y(v): return y0 + h - v / ymax * h
    for gy in range(0, 65, 8):
        yy = Y(gy)
        d.line([(x0, yy), (x0 + w, yy)], fill=C_GRID)
        d.text((x0 - 36, yy - 8), str(gy), fill=C_INK, font=fnt)
    for gx in Ls:
        d.text((X(gx) - 6, y0 + h + 6), str(gx), fill=C_INK, font=fnt)

    # 理论 2^L
    pts = [(X(L), Y(2 ** L)) for L in Ls]
    d.line(pts, fill=C_THEORY, width=2)
    for L in Ls:
        d.ellipse([X(L) - 5, Y(2 ** L) - 5, X(L) + 5, Y(2 ** L) + 5],
                  outline=C_THEORY, width=2)
    # 补零实测
    pts = [(X(L), Y(v)) for L, v in zip(Ls, w_zero)]
    d.line(pts, fill=C_ZERO, width=3)
    for (x, y), v in zip(pts, w_zero):
        d.ellipse([x - 6, y - 6, x + 6, y + 6], fill=C_ZERO)
        d.text((x - 4, y - 26), str(v), fill=C_ZERO, font=fnt)
    # 对称实测（恒 0，贴轴）
    for L in Ls:
        d.ellipse([X(L) - 6, Y(0.8) - 6, X(L) + 6, Y(0.8) + 6], fill=C_SYM)
    d.line([(X(Ls[0]), Y(0.8)), (X(Ls[-1]), Y(0.8))], fill=C_SYM, width=3)

    # 图例放在图下方，避开数据
    lx, ly = x0 + 40, y0 + h + 44
    d.line([(lx, ly), (lx + 28, ly)], fill=C_ZERO, width=3)
    d.ellipse([lx + 11, ly - 5, lx + 21, ly + 5], fill=C_ZERO)
    d.text((lx + 36, ly - 10), "补零延拓实测", fill=C_INK, font=fnt)
    lx2 = lx + 200
    d.line([(lx2, ly), (lx2 + 28, ly)], fill=C_THEORY, width=2)
    d.text((lx2 + 36, ly - 10), "理论 W_L = 2^L", fill=C_INK, font=fnt)
    lx3 = lx2 + 210
    d.line([(lx3, ly), (lx3 + 28, ly)], fill=C_SYM, width=3)
    d.text((lx3 + 36, ly - 10), "对称延拓（恒为 0）", fill=C_INK, font=fnt)
    img.save(os.path.join(FIG, "04_width_vs_levels.png"))


def plot_profiles():
    N = 128
    flat = np.full((N, N), 200.0)
    W, H = 1080, 470
    img = Image.new("RGB", (W, H), C_SURF)
    d = ImageDraw.Draw(img)
    fnt = font(16)
    x0, y0, w, h = 100, 70, 620, 320
    axes(d, x0, y0, w, h, "距左边界像素下标 i", "重建值",
         "左缘剖面：补零暗边逐层向内加深，对称延拓恒为常数（真值=200）", fnt)

    xmax, ylo, yhi = 20, 100, 210
    def X(v): return x0 + v / xmax * w
    def Y(v): return y0 + h - (v - ylo) / (yhi - ylo) * h
    for gy in range(100, 211, 20):
        yy = Y(gy)
        d.line([(x0, yy), (x0 + w, yy)], fill=C_GRID)
        d.text((x0 - 42, yy - 8), str(gy), fill=C_INK, font=fnt)
    for gx in range(0, 21, 2):
        d.text((X(gx) - 6, y0 + h + 6), str(gx), fill=C_INK, font=fnt)
    d.line([(x0, Y(200)), (x0 + w, Y(200))], fill=(90, 90, 90), width=1)
    d.text((x0 + w - 240, Y(200) - 20), "真值 200（水平线）",
           fill=(90, 90, 90), font=fnt)

    xs = np.arange(xmax + 1)
    colors = {1: (210, 170, 150), 2: (190, 120, 95), 3: (160, 75, 60), 4: (120, 35, 30)}
    # 图例放在右侧空白区
    lx = x0 + w + 18
    for k, L in enumerate((1, 2, 3, 4)):
        rz = gaussian_recon(flat, L, "zero")
        prof = rz[N // 2, :xmax + 1]
        pts = [(X(i), Y(v)) for i, v in zip(xs, prof)]
        d.line(pts, fill=colors[L], width=3)
        ly = y0 + 20 + k * 28
        d.line([(lx, ly), (lx + 26, ly)], fill=colors[L], width=3)
        d.text((lx + 32, ly - 9), f"补零 L={L}", fill=colors[L], font=fnt)
    # 对称：所有层重合在 200
    rs = gaussian_recon(flat, 4, "sym")
    prof = rs[N // 2, :xmax + 1]
    pts = [(X(i), Y(v)) for i, v in zip(xs, prof)]
    d.line(pts, fill=C_SYM, width=3)
    ly = y0 + 20 + 4 * 28
    d.line([(lx, ly), (lx + 26, ly)], fill=C_SYM, width=3)
    d.text((lx + 32, ly - 9), "对称 L=1..4（重合）", fill=C_SYM, font=fnt)
    img.save(os.path.join(FIG, "05_boundary_profile.png"))


if __name__ == "__main__":
    plot_widths()
    plot_profiles()
    print("saved 04_width_vs_levels.png, 05_boundary_profile.png")
