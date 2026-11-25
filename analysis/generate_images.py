"""生成三组测试灰度图（256x256，P5 PGM），固定随机种子可复现。"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import numpy as np

from common import write_pgm

W = H = 256
SEED = 42
NOISE = 3.0
OUT = os.path.join(os.path.dirname(__file__), "..", "artifacts", "images")

SPECS = [
    ("img1_gradient.pgm", "渐变场", "gradient"),
    ("img2_shapes.pgm", "分块几何", "shapes"),
    ("img3_texture.pgm", "周期纹理", "texture"),
]


def box_blur(x: np.ndarray, radius: int = 2) -> np.ndarray:
    """半径 radius 的盒式均值模糊（纯 numpy，积分图实现），用于给硬边缘抗锯齿。"""
    k = 2 * radius + 1
    padded = np.pad(x, radius, mode="edge")
    cs = np.cumsum(np.cumsum(padded, axis=0), axis=1)
    cs = np.pad(cs, ((1, 0), (1, 0)), mode="constant")
    return (cs[k:, k:] - cs[:-k, k:] - cs[k:, :-k] + cs[:-k, :-k]) / (k * k)


def build(kind: str, rng: np.random.Generator) -> np.ndarray:
    yy, xx = np.mgrid[0:H, 0:W].astype(np.float64)
    if kind == "gradient":
        x = (40 + 0.45 * xx + 0.25 * yy
             + 30 * np.sin(xx / 32.0) + 20 * np.cos(yy / 24.0))
    elif kind == "shapes":
        # 几何图形先抗锯齿再叠加缓变与噪声，保证任意块长下块内残差不超出
        # 有符号 8 位范围（±127），使增强层可无损表示。
        x = np.full((H, W), 70.0)
        x[(xx - 80) ** 2 + (yy - 90) ** 2 < 55 ** 2] = 170
        x[(xx - 180) ** 2 + (yy - 170) ** 2 < 45 ** 2] = 195
        x[(xx > 120) & (xx < 200) & (yy > 30) & (yy < 110)] = 120
        x = box_blur(x, 2)
        x += 0.12 * xx
    else:
        x = (128 + 26 * np.sin(xx / 14.0)
             + 20 * np.cos(yy / 17.0)
             + 14 * np.sin((xx + yy) / 23.0))
    x += rng.normal(0, NOISE, x.shape)
    return np.clip(x, 0, 255).astype(np.uint8)


def main() -> None:
    os.makedirs(OUT, exist_ok=True)
    rng = np.random.default_rng(SEED)
    for fname, _label, kind in SPECS:
        img = build(kind, rng)
        write_pgm(os.path.join(OUT, fname), img)
        print(f"{fname}: {img.shape} 均值={img.mean():.2f} 标准差={img.std():.2f}")


if __name__ == "__main__":
    main()
