"""合成受控彩色测试图。

画面由四类区域构成, 刻意制造"像素占比"与"色域体积"不一致:
  A 极密窄团(33%): 像素多、sigma 小 -> 占据颜色少, 支撑体积 v 小
  B 中密团(33%):   sigma 中等
  C 稀疏广域(33%): 均匀盒子, 与 A/B 等像素量但铺满大片色域 -> v 大
  D 全域撒点(1%):  制造像素极少、体积极小的长尾格
三类主区像素量基本相等(纯按 p 分配会拿到相近桶数),
但 A/B/C 的支撑体积依次增大, 按 w=pv 分配会把更多桶推向广域。
非空格数控制在 64 以内(4^3 网格), 保证最小档 K=64 每格至少 1 桶。
固定随机种子, 结果可复现。
"""

from __future__ import annotations

import numpy as np

SIZE = 256


def _normal_blob(rng: np.random.Generator, n: int, mean, sigma) -> np.ndarray:
    x = rng.normal(mean, sigma, size=(n, 3))
    return np.clip(np.rint(x), 0, 255).astype(np.uint8)


def _uniform_box(rng: np.random.Generator, n: int, lo, hi) -> np.ndarray:
    x = rng.uniform(lo, hi, size=(n, 3))
    return np.clip(np.rint(x), 0, 255).astype(np.uint8)


def make_image(size: int = SIZE, seed: int = 20261125) -> np.ndarray:
    """返回 (H, W, 3) uint8 图。"""
    rng = np.random.default_rng(seed)
    n_total = size * size

    n_main = int(0.33 * n_total)
    n_d = int(0.01 * n_total)
    n_c = n_total - 2 * n_main - n_d  # C 与 A/B 等量级

    parts = [
        # A 极密窄团: 小 sigma, 实际占据颜色少
        _normal_blob(rng, n_main, mean=(72, 48, 40), sigma=(7, 6, 5)),
        # B 中密团
        _normal_blob(rng, n_main, mean=(188, 168, 64), sigma=(20, 17, 14)),
        # C 稀疏广域: 均匀铺满一个大盒子(青蓝区, 与前两区分开)
        _uniform_box(rng, n_c, lo=(24, 128, 168), hi=(120, 236, 252)),
        # D 全域撒点: 稀疏长尾
        _uniform_box(rng, n_d, lo=(0, 0, 0), hi=(255, 255, 255)),
    ]

    pix = np.concatenate(parts, axis=0)
    rng.shuffle(pix)
    return pix.reshape(size, size, 3).copy()


def save_ppm(path: str, img: np.ndarray) -> None:
    """写 P6 PPM, 无第三方依赖, 便于外部查看。"""
    h, w, _ = img.shape
    header = f"P6\n{w} {h}\n255\n".encode("ascii")
    with open(path, "wb") as f:
        f.write(header)
        f.write(np.ascontiguousarray(img).tobytes())
