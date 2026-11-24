#!/usr/bin/env python3
# 把改造前后三张抖动 PGM 渲染成一张对比 PNG：
# 左列完整图，右列灰场区左上角 64x64 放大（规则纹最直观）。
# 运行: python3 -I analysis/render_compare.py
from PIL import Image, ImageDraw, ImageFont
import os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ART = os.path.join(ROOT, "artifacts", "dither")

MATRICES = [
    ("bayer", "M1 Bayer (before: original matrix, regular checkerboard)"),
    ("void-cluster", "M2 void-and-cluster (blue-noise)"),
    ("spectral", "M3 spectral (after: LF-leakage optimized, irregular)"),
]
ZOOM = 64
SCALE = 3  # 放大裁切的最近邻倍数


def read_pgm(path):
    with open(path, "rb") as f:
        data = f.read()
    idx = 0
    tokens = []
    # 跳过 P5、宽、高、maxval 四个 token
    while len(tokens) < 4:
        while data[idx] in b" \t\n\r":
            idx += 1
        start = idx
        while data[idx] not in b" \t\n\r":
            idx += 1
        tokens.append(data[start:idx])
    idx += 1  # 单字节分隔符
    w, h = int(tokens[1]), int(tokens[2])
    return Image.frombytes("L", (w, h), data[idx:idx + w * h])


def main():
    font = ImageFont.load_default()
    full_imgs = [(name, title, read_pgm(os.path.join(ART, f"dither_{name}.pgm")))
                 for name, title in MATRICES]
    w, h = full_imgs[0][2].size
    pad, label_h = 16, 28
    panel_w = w + ZOOM * SCALE + pad * 3
    panel_h = (h + label_h + pad) * len(full_imgs) + pad
    canvas = Image.new("L", (panel_w, panel_h), 255)
    draw = ImageDraw.Draw(canvas)

    y = pad
    for name, title, img in full_imgs:
        draw.text((pad, y + 8), title, fill=0, font=font)
        canvas.paste(img, (pad, y + label_h))
        # 灰场区（上半）左上角 64x64 放大
        crop = img.crop((0, 0, ZOOM, ZOOM)).resize(
            (ZOOM * SCALE, ZOOM * SCALE), Image.NEAREST)
        cx = pad * 2 + w
        canvas.paste(crop, (cx, y + label_h))
        draw.text((cx, y + 8), "flat-128 crop 64x64, x3 nearest", fill=0, font=font)
        y += h + label_h + pad

    out = os.path.join(ART, "compare.png")
    canvas.save(out)
    print("wrote", out, canvas.size)


if __name__ == "__main__":
    main()
